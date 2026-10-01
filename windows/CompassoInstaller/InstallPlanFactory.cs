using System.Text.Json;

namespace CompassoInstaller;

/// <summary>
/// Monta o plano de instalação a partir do manifesto que viaja no pacote.
///
/// O manifesto é a fronteira entre o que o instalador sabe fazer e o que ele
/// transporta. O motor não conhece nenhuma interface: ele lê arquivos, cria
/// atalhos e registra a desinstalação. Por isso o mesmo motor serve para a
/// primeira interface e para as próximas, sem nova alteração de código.
/// </summary>
internal static class InstallPlanFactory
{
    internal const string ManifestFileName = "payload.json";

    private static readonly JsonSerializerOptions ManifestOptions = new()
    {
        PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
        ReadCommentHandling = JsonCommentHandling.Skip,
    };

    /// <summary>
    /// Devolve o plano, ou <c>null</c> quando o pacote ainda não traz nada
    /// instalável. Preferimos um <c>null</c> explícito a um plano vazio: a
    /// interface precisa distinguir "não há componente" de "instalou tudo".
    /// </summary>
    internal static InstallPlan? TryCreatePlan(string payloadDirectory)
    {
        var manifestPath = Path.Combine(payloadDirectory, ManifestFileName);
        if (!File.Exists(manifestPath))
        {
            return null;
        }

        var plan = JsonSerializer.Deserialize<InstallPlan>(
            File.ReadAllText(manifestPath),
            ManifestOptions);

        if (plan is null || plan.Files.Count == 0)
        {
            return null;
        }

        if (string.IsNullOrWhiteSpace(plan.ProductName))
        {
            plan.ProductName = "Compasso";
        }

        if (string.IsNullOrWhiteSpace(plan.InstallDirectory))
        {
            plan.InstallDirectory = InstallPaths.InstallDirectory;
        }

        if (string.IsNullOrWhiteSpace(plan.StartMenuDirectory))
        {
            plan.StartMenuDirectory = InstallPaths.StartMenuDirectory;
        }

        if (string.IsNullOrWhiteSpace(plan.Version))
        {
            plan.Version = "0.1";
        }

        if (string.IsNullOrWhiteSpace(plan.Uninstall.KeyPath))
        {
            plan.Uninstall.KeyPath = InstallPaths.UninstallKeyPath;
        }

        if (PortableSetup.IsAvailable)
        {
            plan.Uninstall.UninstallString = $"\"{InstallPaths.InstalledSetupPath}\"";
        }

        return plan;
    }

    internal static void SetDesktopShortcut(InstallPlan plan, bool enabled)
    {
        plan.Shortcuts.RemoveAll(shortcut => string.Equals(
            Path.GetFullPath(shortcut.ShortcutPath),
            Path.GetFullPath(InstallPaths.DesktopShortcutPath),
            StringComparison.OrdinalIgnoreCase));

        if (!enabled)
        {
            return;
        }

        var startMenuShortcut = plan.Shortcuts.FirstOrDefault();
        if (startMenuShortcut is null)
        {
            throw new InvalidOperationException("O pacote não contém o atalho principal do Compasso.");
        }

        plan.Shortcuts.Add(new PayloadShortcut
        {
            Target = startMenuShortcut.Target,
            ShortcutPath = InstallPaths.DesktopShortcutPath,
            Description = startMenuShortcut.Description,
            IconLocation = startMenuShortcut.IconLocation,
        });
    }
}
