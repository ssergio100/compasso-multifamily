using System.Diagnostics;
using System.IO.Pipes;
using System.Text;
using System.Text.Json;
using Microsoft.Win32;

namespace CompassoInstaller;

/// <summary>
/// Processo elevado de longa duração. Nasceu da antiga sonda de elevação, que
/// só dizia se tinha permissão; agora continua vivo depois de responder e passa
/// a aplicar o plano de instalação que a interface não elevada enviar pelo
/// mesmo pipe.
///
/// O processo não cria janela. Entra por <c>App.OnLaunched</c>, atende comandos
/// até receber <see cref="MessageType.Shutdown"/> e sai com código 0.
/// </summary>
internal static class ElevatedWorker
{
    private const int ExitOk = 0;
    private const int ExitNotElevated = 1;
    private const int ExitFaulted = 2;

    internal static async Task<int> RunAsync(string pipeName, CancellationToken cancellationToken)
    {
        if (!ElevationVerifier.IsProcessElevated())
        {
            return ExitNotElevated;
        }

        try
        {
            using var client = new NamedPipeClientStream(
                ".",
                pipeName,
                PipeDirection.InOut,
                PipeOptions.Asynchronous);

            await client.ConnectAsync(cancellationToken);

            await using var writer = new StreamWriter(client, new UTF8Encoding(false), 1024, leaveOpen: true)
            {
                AutoFlush = true,
                NewLine = "\n",
            };
            using var reader = new StreamReader(client, Encoding.UTF8, true, 1024, leaveOpen: true);

            await SendAsync(
                writer,
                new WorkerMessage
                {
                    Type = MessageType.Ready,
                    Elevated = true,
                    Text = "Permissão administrativa ativa.",
                },
                cancellationToken);

            while (!cancellationToken.IsCancellationRequested)
            {
                var line = await reader.ReadLineAsync(cancellationToken);
                if (line is null)
                {
                    // A interface fechou o canal. Nada a fazer além de encerrar.
                    break;
                }

                WorkerMessage? request;
                try
                {
                    request = JsonSerializer.Deserialize(line, WorkerMessageContext.Default.WorkerMessage);
                }
                catch (JsonException)
                {
                    continue;
                }

                if (request is null)
                {
                    continue;
                }

                if (string.Equals(request.Type, MessageType.Shutdown, StringComparison.Ordinal))
                {
                    break;
                }

                if (string.Equals(request.Type, MessageType.Install, StringComparison.Ordinal))
                {
                    await HandleInstallAsync(writer, request.Plan, cancellationToken);
                    continue;
                }

                if (string.Equals(request.Type, MessageType.Uninstall, StringComparison.Ordinal))
                {
                    await HandleUninstallAsync(writer, request.Plan, cancellationToken);
                }
            }

            return ExitOk;
        }
        catch (OperationCanceledException)
        {
            return ExitOk;
        }
        catch (Exception exception)
        {
            Log($"Elevated worker failed: {exception}");
            return ExitFaulted;
        }
    }

    private static async Task HandleInstallAsync(
        StreamWriter writer,
        InstallPlan? plan,
        CancellationToken cancellationToken)
    {
        if (plan is null)
        {
            await SendAsync(
                writer,
                new WorkerMessage
                {
                    Type = MessageType.Fault,
                    Succeeded = false,
                    Text = "O plano de instalação chegou vazio.",
                },
                cancellationToken);
            return;
        }

        try
        {
            var executor = new InstallPlanExecutor(plan, AppContext.BaseDirectory);
            await executor.ApplyAsync(
                (step, percent) => SendAsync(
                    writer,
                    new WorkerMessage
                    {
                        Type = MessageType.Progress,
                        Percent = percent,
                        Text = step,
                    },
                    cancellationToken),
                cancellationToken);

            await SendAsync(
                writer,
                new WorkerMessage
                {
                    Type = MessageType.Result,
                    Succeeded = true,
                    Percent = 100,
                    Text = $"{plan.ProductName} {plan.Version} foi instalado.",
                },
                cancellationToken);
        }
        catch (Exception exception)
        {
            Log($"Install plan failed: {exception}");
            await SendAsync(
                writer,
                new WorkerMessage
                {
                    Type = MessageType.Fault,
                    Succeeded = false,
                    Text = $"A instalação falhou: {exception.Message}",
                },
                cancellationToken);
        }
    }

    private static async Task HandleUninstallAsync(
        StreamWriter writer,
        InstallPlan? plan,
        CancellationToken cancellationToken)
    {
        if (plan is null)
        {
            await SendAsync(
                writer,
                new WorkerMessage
                {
                    Type = MessageType.Fault,
                    Succeeded = false,
                    Text = "O plano de desinstalação chegou vazio.",
                },
                cancellationToken);
            return;
        }

        try
        {
            var executor = new UninstallPlanExecutor(plan);
            await executor.ApplyAsync(
                (step, percent) => SendAsync(
                    writer,
                    new WorkerMessage
                    {
                        Type = MessageType.Progress,
                        Percent = percent,
                        Text = step,
                    },
                    cancellationToken),
                cancellationToken);

            await SendAsync(
                writer,
                new WorkerMessage
                {
                    Type = MessageType.Result,
                    Succeeded = true,
                    Percent = 100,
                    Text = $"{plan.ProductName} foi desinstalado deste computador.",
                },
                cancellationToken);
        }
        catch (Exception exception)
        {
            Log($"Uninstall plan failed: {exception}");
            await SendAsync(
                writer,
                new WorkerMessage
                {
                    Type = MessageType.Fault,
                    Succeeded = false,
                    Text = $"A desinstalação falhou: {exception.Message}",
                },
                cancellationToken);
        }
    }

    private static async Task SendAsync(StreamWriter writer, WorkerMessage message, CancellationToken cancellationToken)
    {
        var json = JsonSerializer.Serialize(message, WorkerMessageContext.Default.WorkerMessage);
        await writer.WriteLineAsync(json.AsMemory(), cancellationToken);
    }

    private static void Log(string message)
    {
#if DEBUG
        var path = Path.Combine(AppContext.BaseDirectory, "startup.log");
        File.AppendAllText(path, $"{DateTimeOffset.Now:O} {message}{Environment.NewLine}");
#endif
    }
}
