using System.Diagnostics;
using Microsoft.Win32;

namespace CompassoInstaller;

/// <summary>
/// Remove o que a instalação gravou. Roda exclusivamente dentro do processo
/// elevado, pelo mesmo caminho do <see cref="InstallPlanExecutor"/>.
///
/// A remoção é feita por diretório inteiro, e não arquivo por arquivo do
/// manifesto: um plano desatualizado deixaria lixo para trás, e o objetivo aqui
/// é justamente deixar o computador como estava antes da instalação. Por isso
/// o plano também declara os diretórios a remover, e a remoção é recusada se
/// algum deles não for um dos caminhos que o instalador controla.
/// </summary>
internal sealed class UninstallPlanExecutor
{
    private readonly InstallPlan _plan;

    internal UninstallPlanExecutor(InstallPlan plan)
    {
        _plan = plan;
    }

    internal async Task ApplyAsync(Func<string, int, Task> report, CancellationToken cancellationToken)
    {
        var steps = BuildSteps();
        var completed = 0;

        foreach (var step in steps)
        {
            cancellationToken.ThrowIfCancellationRequested();

            var percent = (int)(100.0 * completed / steps.Count);
            await report(step.Label, percent);

            step.Action();
            completed++;

            await report(step.Label, (int)(100.0 * completed / steps.Count));
        }
    }

    private List<(string Label, Action Action)> BuildSteps()
    {
        var steps = new List<(string, Action)>
        {
            ("Verificando o que está em uso", EnsureNotRunning),
        };

        steps.Add(($"Removendo {_plan.InstallDirectory}", RemoveInstallDirectory));
        steps.Add(("Removendo os atalhos do menu Iniciar", RemoveStartMenu));
        steps.Add(("Removendo o atalho da área de trabalho", RemoveDesktopShortcut));
        steps.Add(("Removendo o registro da desinstalação", RemoveRegistryEntry));

        return steps;
    }

    /// <summary>
    /// Um executável aberto impede a remoção dos próprios arquivos, e a falha
    /// apareceria só no meio da desinstalação. Melhor dizer antes.
    /// </summary>
    private void EnsureNotRunning()
    {
        if (!Directory.Exists(_plan.InstallDirectory))
        {
            return;
        }

        var target = Path.Combine(_plan.InstallDirectory, "CompassoApp.exe");
        if (!File.Exists(target))
        {
            return;
        }

        var running = Process.GetProcessesByName("CompassoApp");
        if (running.Length == 0)
        {
            return;
        }

        foreach (var process in running)
        {
            process.Dispose();
        }

        throw new InvalidOperationException(
            "Feche o aplicativo Compasso antes de desinstalar. A desinstalação não pode remover um programa em uso.");
    }

    private void RemoveInstallDirectory()
    {
        var directory = RequireControlledPath(_plan.InstallDirectory, InstallPaths.InstallDirectory);
        DeleteDirectoryIfExists(directory);
    }

    private void RemoveStartMenu()
    {
        var directory = RequireControlledPath(_plan.StartMenuDirectory, InstallPaths.StartMenuDirectory);
        DeleteDirectoryIfExists(directory);
    }

    private static void RemoveDesktopShortcut()
    {
        if (File.Exists(InstallPaths.DesktopShortcutPath))
        {
            File.Delete(InstallPaths.DesktopShortcutPath);
        }
    }

    private void RemoveRegistryEntry()
    {
        if (string.IsNullOrWhiteSpace(_plan.Uninstall.KeyPath))
        {
            return;
        }

        using var hive = RegistryKey.OpenBaseKey(RegistryHive.LocalMachine, RegistryView.Registry64);
        hive.DeleteSubKeyTree(_plan.Uninstall.KeyPath, throwOnMissingSubKey: false);
    }

    /// <summary>
    /// Só remove os dois diretórios que o instalador controla. Um plano vindo do
    /// pacote não pode apontar a remoção para outro lugar do disco.
    /// </summary>
    private static string RequireControlledPath(string candidate, string expected)
    {
        if (string.IsNullOrWhiteSpace(candidate))
        {
            throw new InvalidOperationException("O plano não informa o diretório a remover.");
        }

        var normalized = Path.GetFullPath(candidate).TrimEnd('\\');
        var allowed = Path.GetFullPath(expected).TrimEnd('\\');

        if (!string.Equals(normalized, allowed, StringComparison.OrdinalIgnoreCase))
        {
            throw new InvalidOperationException(
                $"A desinstalação recusou remover \"{normalized}\": apenas \"{allowed}\" é permitido.");
        }

        return normalized;
    }

    private static void DeleteDirectoryIfExists(string directory)
    {
        if (!Directory.Exists(directory))
        {
            return;
        }

        // Arquivos vindos de um pacote podem chegar somente-leitura, e o que não
        // é removido ficaria para trás depois da desinstalação.
        foreach (var file in Directory.EnumerateFiles(directory, "*", SearchOption.AllDirectories))
        {
            var attributes = File.GetAttributes(file);
            if (attributes.HasFlag(FileAttributes.ReadOnly))
            {
                File.SetAttributes(file, attributes & ~FileAttributes.ReadOnly);
            }
        }

        Directory.Delete(directory, recursive: true);
    }
}
