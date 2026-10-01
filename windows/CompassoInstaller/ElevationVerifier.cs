using System.Security.Principal;

namespace CompassoInstaller;

/// <summary>
/// Utilitários de elevação compartilhados entre a interface e o trabalhador.
/// A responsabilidade de elevar e de aplicar o plano saiu daqui para
/// <see cref="InstallEngine"/> e <see cref="InstallPlanExecutor"/>.
/// </summary>
internal static class ElevationVerifier
{
    /// <summary>
    /// Reconhece a invocação de trabalhador elevado. O nome do argumento foi
    /// mantido do item 4 para não invalidar a validação já feita: o que mudou é
    /// o que o processo faz depois de conectar, não como ele é chamado.
    /// </summary>
    internal static bool TryGetProbePipeName(string[] arguments, out string pipeName)
    {
        pipeName = string.Empty;
        if (arguments.Length != 3 || arguments[1] != InstallProtocol.WorkerArgument)
        {
            return false;
        }

        var candidate = arguments[2];
        if (!candidate.StartsWith(InstallProtocol.PipePrefix, StringComparison.Ordinal))
        {
            return false;
        }

        if (!Guid.TryParseExact(candidate[InstallProtocol.PipePrefix.Length..], "N", out _))
        {
            return false;
        }

        pipeName = candidate;
        return true;
    }

    internal static bool IsProcessElevated()
    {
        using var identity = WindowsIdentity.GetCurrent();
        return new WindowsPrincipal(identity).IsInRole(WindowsBuiltInRole.Administrator);
    }
}
