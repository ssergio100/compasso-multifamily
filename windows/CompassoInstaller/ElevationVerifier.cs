using System.ComponentModel;
using System.Diagnostics;
using System.IO.Pipes;
using System.Security.Principal;

namespace CompassoInstaller;

internal enum ElevationVerificationStatus
{
    Confirmed,
    Cancelled,
    Failed,
}

internal sealed record ElevationVerificationResult(
    ElevationVerificationStatus Status,
    string Message);

internal static class ElevationVerifier
{
    private const string ProbeArgument = "--elevated-probe";
    private const string PipePrefix = "compasso-installer-";
    private const int UacCancelledErrorCode = 1223;

    internal static bool TryGetProbePipeName(string[] arguments, out string pipeName)
    {
        pipeName = string.Empty;
        if (arguments.Length != 3 || arguments[1] != ProbeArgument)
        {
            return false;
        }

        var candidate = arguments[2];
        if (!candidate.StartsWith(PipePrefix, StringComparison.Ordinal))
        {
            return false;
        }

        var token = candidate[PipePrefix.Length..];
        if (!Guid.TryParseExact(token, "N", out _))
        {
            return false;
        }

        pipeName = candidate;
        return true;
    }

    internal static async Task<int> RunProbeAsync(string pipeName)
    {
        try
        {
            using var client = new NamedPipeClientStream(
                ".",
                pipeName,
                PipeDirection.Out,
                PipeOptions.Asynchronous);
            await client.ConnectAsync(15_000);

            await using var writer = new StreamWriter(client)
            {
                AutoFlush = true,
            };

            var isElevated = IsProcessElevated();
            await writer.WriteLineAsync(isElevated ? "elevated" : "not-elevated");
            return isElevated ? 0 : 1;
        }
        catch (Exception exception)
        {
#if DEBUG
            var path = System.IO.Path.Combine(AppContext.BaseDirectory, "startup.log");
            File.AppendAllText(path, $"{DateTimeOffset.Now:O} Elevation probe failed: {exception}{Environment.NewLine}");
#endif
            return 2;
        }
    }

    internal static async Task<ElevationVerificationResult> RequestAsync()
    {
        if (IsProcessElevated())
        {
            return new ElevationVerificationResult(
                ElevationVerificationStatus.Confirmed,
                "A permissão de administrador já está ativa para este instalador.");
        }

        var pipeName = $"{PipePrefix}{Guid.NewGuid():N}";
        using var server = new NamedPipeServerStream(
            pipeName,
            PipeDirection.In,
            1,
            PipeTransmissionMode.Byte,
            PipeOptions.Asynchronous | PipeOptions.CurrentUserOnly);

        Process? elevatedProcess;
        try
        {
            var executablePath = Environment.ProcessPath
                ?? throw new InvalidOperationException("Não foi possível localizar o executável do instalador.");

            elevatedProcess = Process.Start(new ProcessStartInfo
            {
                FileName = executablePath,
                Arguments = $"{ProbeArgument} {pipeName}",
                UseShellExecute = true,
                Verb = "runas",
                WorkingDirectory = AppContext.BaseDirectory,
            });
        }
        catch (Win32Exception exception) when (exception.NativeErrorCode == UacCancelledErrorCode)
        {
            return new ElevationVerificationResult(
                ElevationVerificationStatus.Cancelled,
                "A permissão foi cancelada. Nada foi alterado no computador.");
        }
        catch (Exception exception)
        {
            return new ElevationVerificationResult(
                ElevationVerificationStatus.Failed,
                $"Não foi possível solicitar a permissão de administrador: {exception.Message}");
        }

        if (elevatedProcess is null)
        {
            return new ElevationVerificationResult(
                ElevationVerificationStatus.Failed,
                "O Windows não iniciou a verificação administrativa.");
        }

        using (elevatedProcess)
        using (var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(60)))
        {
            try
            {
                var connectionTask = server.WaitForConnectionAsync(timeout.Token);
                var exitTask = elevatedProcess.WaitForExitAsync(timeout.Token);
                var completedTask = await Task.WhenAny(connectionTask, exitTask);

                if (completedTask == exitTask && !server.IsConnected)
                {
                    return new ElevationVerificationResult(
                        ElevationVerificationStatus.Failed,
                        "A verificação administrativa terminou antes de responder.");
                }

                await connectionTask;
                using var reader = new StreamReader(server);
                var response = await reader.ReadLineAsync(timeout.Token);
                await exitTask;

                if (response == "elevated" && elevatedProcess.ExitCode == 0)
                {
                    return new ElevationVerificationResult(
                        ElevationVerificationStatus.Confirmed,
                        "Permissão confirmada. O instalador está pronto para receber os componentes nas próximas etapas.");
                }

                return new ElevationVerificationResult(
                    ElevationVerificationStatus.Failed,
                    "O processo iniciado não recebeu permissão de administrador.");
            }
            catch (OperationCanceledException)
            {
                return new ElevationVerificationResult(
                    ElevationVerificationStatus.Failed,
                    "A verificação administrativa não respondeu dentro do tempo esperado.");
            }
            catch (Exception exception)
            {
                return new ElevationVerificationResult(
                    ElevationVerificationStatus.Failed,
                    $"Falha durante a verificação administrativa: {exception.Message}");
            }
        }
    }

    private static bool IsProcessElevated()
    {
        using var identity = WindowsIdentity.GetCurrent();
        var principal = new WindowsPrincipal(identity);
        return principal.IsInRole(WindowsBuiltInRole.Administrator);
    }
}
