namespace CompassoInstaller;

/// <summary>Instala a cópia permanente do setup externo de arquivo único.</summary>
internal static class PortableSetup
{
    internal const string SourceEnvironmentVariable = "COMPASSO_SETUP_SOURCE";

    internal static string? SourcePath => Environment.GetEnvironmentVariable(SourceEnvironmentVariable);

    internal static bool IsAvailable =>
        !string.IsNullOrWhiteSpace(SourcePath) && File.Exists(SourcePath);

    internal static void CopyToStaging(string stagingDirectory)
    {
        if (!IsAvailable)
        {
            return;
        }

        var destination = Path.Combine(stagingDirectory, "Installer", "CompassoSetup.exe");
        Directory.CreateDirectory(Path.GetDirectoryName(destination)!);
        File.Copy(SourcePath!, destination, overwrite: true);
    }
}
