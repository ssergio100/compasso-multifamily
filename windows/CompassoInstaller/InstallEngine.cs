using System.ComponentModel;
using System.Diagnostics;
using System.IO.Pipes;
using System.Text;
using System.Text.Json;

namespace CompassoInstaller;

internal enum ElevateStatus
{
    Connected,
    AlreadyElevated,
    Cancelled,
    Failed,
}

internal sealed record ElevateResult(
    ElevateStatus Status,
    ElevatedSession? Session,
    string Message);

/// <summary>
/// Lado não elevado do canal. Pede a elevação uma única vez, mantém o processo
/// elevado vivo e traduz as mensagens dele em progresso para a interface.
///
/// A bomba de mensagens é a parte que torna o canal duplex de verdade: o mesmo
/// pipe que traz o resultado traz o progresso, e o mesmo que envia o comando
/// fica aberto enquanto a instalação corre.
/// </summary>
internal sealed class InstallEngine
{
    private const int UacCancelledErrorCode = 1223;
    private static readonly TimeSpan ConnectTimeout = InstallProtocol.ConnectTimeout;
    private static readonly TimeSpan HandshakeTimeout = InstallProtocol.HandshakeTimeout;

    private InstallEngine()
    {
    }

    internal static async Task<ElevateResult> ConnectAsync()
    {
        var wasAlreadyElevated = ElevationVerifier.IsProcessElevated();
        var pipeName = $"{InstallProtocol.PipePrefix}{Guid.NewGuid():N}";

        // Sem `using`: a sessão fica dona do pipe e o encerra no descarte.
        var server = new NamedPipeServerStream(
            pipeName,
            PipeDirection.InOut,
            1,
            PipeTransmissionMode.Byte,
            PipeOptions.Asynchronous | PipeOptions.CurrentUserOnly);

        var lifetime = new CancellationTokenSource();
        Process? elevatedProcess = null;
        Task<int>? inProcessWorker = null;

        try
        {
            if (wasAlreadyElevated)
            {
                // Já temos o privilégio. Rodar o trabalhador dentro deste processo
                // evita um segundo processo sem mudar o caminho de execução.
                inProcessWorker = Task.Run(
                    () => ElevatedWorker.RunAsync(pipeName, lifetime.Token),
                    lifetime.Token);
            }
            else
            {
                var executablePath = Environment.ProcessPath
                    ?? throw new InvalidOperationException("Não foi possível localizar o executável do instalador.");

                elevatedProcess = await Task.Run(() => Process.Start(new ProcessStartInfo
                {
                    FileName = executablePath,
                    Arguments = $"{InstallProtocol.WorkerArgument} {pipeName}",
                    UseShellExecute = true,
                    Verb = "runas",
                    WorkingDirectory = AppContext.BaseDirectory,
                }));
            }

            if (!wasAlreadyElevated && elevatedProcess is null)
            {
                return new ElevateResult(
                    ElevateStatus.Failed,
                    null,
                    "O Windows não iniciou o processo de instalação.");
            }
        }
        catch (Win32Exception exception) when (exception.NativeErrorCode == UacCancelledErrorCode)
        {
            lifetime.Cancel();
            return new ElevateResult(
                ElevateStatus.Cancelled,
                null,
                "A permissão foi cancelada. Nada foi alterado no computador.");
        }
        catch (Exception exception)
        {
            lifetime.Cancel();
            return new ElevateResult(
                ElevateStatus.Failed,
                null,
                $"Não foi possível solicitar a permissão de administrador: {exception.Message}");
        }

        // O cliente só conecta depois que a instância do servidor existe, mas
        // esperar a conexão explicitamente deixa o caso "o processo elevado
        // morreu antes de falar" distinguível de "ninguém respondeu".
        try
        {
            using var connectTimeout = new CancellationTokenSource(ConnectTimeout);
            var connection = server.WaitForConnectionAsync(connectTimeout.Token);

            if (elevatedProcess is not null)
            {
                var exited = elevatedProcess.WaitForExitAsync(connectTimeout.Token);
                var first = await Task.WhenAny(connection, exited);
                if (first == exited && !server.IsConnected)
                {
                    await connectTimeout.CancelAsync();
                    return new ElevateResult(
                        ElevateStatus.Failed,
                        null,
                        $"O processo autorizado terminou antes de conectar (código {elevatedProcess.ExitCode}).");
                }
            }

            using (var forced = CancellationTokenSource.CreateLinkedTokenSource(connectTimeout.Token))
            {
                forced.CancelAfter(ConnectTimeout);
                await connection.WaitAsync(forced.Token);
            }
        }
        catch (Exception)
        {
            await lifetime.CancelAsync();
            elevatedProcess?.Dispose();
            server.Dispose();
            lifetime.Dispose();
            return new ElevateResult(
                ElevateStatus.Failed,
                null,
                "O processo autorizado não conseguiu abrir a comunicação.");
        }

        var session = new ElevatedSession(server, lifetime, elevatedProcess, inProcessWorker);

        var ready = await session.WaitForReadyAsync(HandshakeTimeout);
        if (ready is null)
        {
            await session.DisposeAsync();
            return new ElevateResult(
                ElevateStatus.Failed,
                null,
                "O processo autorizado não respondeu a tempo.");
        }

        return new ElevateResult(
            wasAlreadyElevated ? ElevateStatus.AlreadyElevated : ElevateStatus.Connected,
            session,
            wasAlreadyElevated
                ? "A permissão de administrador já está ativa para este instalador."
                : "Permissão confirmada. O instalador está autorizado a gravar no sistema.");
    }
}

/// <summary>
/// Uma sessão duplex viva. Mantém a bomba de leitura correndo, entrega progresso
/// por evento e resolve cada comando com um resultado terminal único.
/// </summary>
internal sealed class ElevatedSession : IAsyncDisposable
{
    private readonly NamedPipeServerStream _pipe;
    private readonly CancellationTokenSource _lifetime;
    private readonly Process? _process;
    private readonly Task<int>? _inProcessWorker;
    private readonly StreamWriter _writer;
    private readonly TaskCompletionSource<WorkerMessage> _ready =
        new(TaskCreationOptions.RunContinuationsAsynchronously);

    private readonly object _gate = new();
    private TaskCompletionSource<WorkerMessage>? _pending;
    private Task? _pump;
    private bool _disposed;

    internal ElevatedSession(
        NamedPipeServerStream pipe,
        CancellationTokenSource lifetime,
        Process? process,
        Task<int>? inProcessWorker)
    {
        _pipe = pipe;
        _lifetime = lifetime;
        _process = process;
        _inProcessWorker = inProcessWorker;
        _writer = new StreamWriter(pipe, new UTF8Encoding(false), 1024, leaveOpen: true)
        {
            AutoFlush = true,
            NewLine = "\n",
        };
    }

    /// <summary>Relata progresso e falhas intermediárias vindas do trabalhador elevado.</summary>
    internal event EventHandler<WorkerMessage>? Notified;

    internal bool Elevated { get; private set; }

    internal async Task<WorkerMessage?> WaitForReadyAsync(TimeSpan timeout)
    {
        _pump = PumpAsync();

        try
        {
            using var cancellation = new CancellationTokenSource(timeout);
            var completed = await Task.WhenAny(_ready.Task, Task.Delay(Timeout.Infinite, cancellation.Token));
            if (completed == _ready.Task)
            {
                return _ready.Task.Result;
            }

            return null;
        }
        catch (OperationCanceledException)
        {
            return null;
        }
    }

    internal async Task<WorkerMessage> InstallAsync(InstallPlan plan, CancellationToken cancellationToken)
    {
        return await SendOperationAsync(
            new WorkerMessage { Type = MessageType.Install, Plan = plan },
            "A instalação",
            cancellationToken);
    }

    internal async Task<WorkerMessage> UninstallAsync(InstallPlan plan, CancellationToken cancellationToken)
    {
        return await SendOperationAsync(
            new WorkerMessage { Type = MessageType.Uninstall, Plan = plan },
            "A desinstalação",
            cancellationToken);
    }

    /// <summary>
    /// Envia um comando de operação e espera a resposta terminal do trabalhador.
    /// Instalação e desinstalação diferem só no comando e no texto de espera, e
    /// o resto do protocolo é idêntico.
    /// </summary>
    private async Task<WorkerMessage> SendOperationAsync(
        WorkerMessage request,
        string operationName,
        CancellationToken cancellationToken)
    {
        var completion = new TaskCompletionSource<WorkerMessage>(TaskCreationOptions.RunContinuationsAsynchronously);
        lock (_gate)
        {
            _pending = completion;
        }

        await SendAsync(request, cancellationToken);

        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        timeout.CancelAfter(InstallProtocol.OperationTimeout);

        try
        {
            return await completion.Task.WaitAsync(timeout.Token);
        }
        catch (OperationCanceledException) when (!cancellationToken.IsCancellationRequested)
        {
            throw new TimeoutException($"{operationName} não respondeu dentro do tempo esperado.");
        }
        finally
        {
            lock (_gate)
            {
                _pending = null;
            }
        }
    }

    internal async Task ShutdownAsync()
    {
        if (_disposed)
        {
            return;
        }

        try
        {
            using var timeout = new CancellationTokenSource(InstallProtocol.ShutdownTimeout);
            await SendAsync(new WorkerMessage { Type = MessageType.Shutdown }, timeout.Token);
        }
        catch (Exception)
        {
            // Encerrar é o melhor esforço. Se o canal já estiver morto, o descarte
            // abaixo encerra o processo do trabalhador de qualquer forma.
        }
    }

    private async Task SendAsync(WorkerMessage message, CancellationToken cancellationToken)
    {
        var json = JsonSerializer.Serialize(message, WorkerMessageContext.Default.WorkerMessage);
        await _writer.WriteLineAsync(json.AsMemory(), cancellationToken);
    }

    private async Task PumpAsync()
    {
        try
        {
            using var reader = new StreamReader(_pipe, Encoding.UTF8, true, 1024, leaveOpen: true);

            while (await reader.ReadLineAsync(_lifetime.Token) is { } line)
            {
                WorkerMessage? message;
                try
                {
                    message = JsonSerializer.Deserialize(line, WorkerMessageContext.Default.WorkerMessage);
                }
                catch (JsonException)
                {
                    continue;
                }

                if (message is null)
                {
                    continue;
                }

                TraceMessage(message);

                if (string.Equals(message.Type, MessageType.Ready, StringComparison.Ordinal))
                {
                    Elevated = message.Elevated;
                    _ready.TrySetResult(message);
                    continue;
                }

                if (string.Equals(message.Type, MessageType.Result, StringComparison.Ordinal)
                    || string.Equals(message.Type, MessageType.Fault, StringComparison.Ordinal))
                {
                    TaskCompletionSource<WorkerMessage>? pending;
                    lock (_gate)
                    {
                        pending = _pending;
                    }

                    pending?.TrySetResult(message);
                    continue;
                }

                Notified?.Invoke(this, message);
            }
        }
        catch (OperationCanceledException)
        {
        }
        catch (Exception exception)
        {
            _ready.TrySetException(exception);
        }
        finally
        {
            _ready.TrySetResult(new WorkerMessage { Type = MessageType.Fault, Text = "O canal foi encerrado." });
        }
    }

    /// <summary>
    /// Grava cada mensagem recebida em <c>startup.log</c> nas builds de
    /// depuração. Sem isso não há como provar, depois do fato, que o progresso
    /// chegou em vários eventos e não apenas no resultado final.
    /// </summary>
    private static void TraceMessage(WorkerMessage message)
    {
#if DEBUG
        try
        {
            var path = Path.Combine(AppContext.BaseDirectory, "startup.log");
            File.AppendAllText(path, $"{DateTimeOffset.Now:O} RX {message.Type} {message.Percent} {message.Text}{Environment.NewLine}");
        }
        catch (Exception)
        {
        }
#endif
    }

    public async ValueTask DisposeAsync()
    {
        if (_disposed)
        {
            return;
        }

        _disposed = true;
        await _lifetime.CancelAsync();

        try
        {
            if (_pump is not null)
            {
                await _pump.WaitAsync(TimeSpan.FromSeconds(2));
            }
        }
        catch (Exception)
        {
        }

        _writer.Dispose();
        _pipe.Dispose();

        if (_process is not null)
        {
            try
            {
                if (!_process.WaitForExit(1000))
                {
                    _process.Kill(entireProcessTree: true);
                }
            }
            catch (Exception)
            {
            }

            _process.Dispose();
        }

        if (_inProcessWorker is not null)
        {
            try
            {
                await _inProcessWorker.WaitAsync(TimeSpan.FromSeconds(2));
            }
            catch (Exception)
            {
            }
        }

        _lifetime.Dispose();
    }
}
