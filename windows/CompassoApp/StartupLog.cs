namespace CompassoApp;

/// <summary>
/// Log de inicializacao do aplicativo.
///
/// O log vai para %LOCALAPPDATA%\Compasso, nunca para a pasta do executavel.
/// A pasta de instalacao fica em C:\Program Files, que o usuario que executa o
/// aplicativo nao pode gravar, e uma gravacao negada no construtor do App
/// derrubava a aplicacao inteira antes da janela existir.
///
/// Alem disso toda escrita e engolida: um log de diagnostico que consegue
/// derrubar o aplicativo e pior do que nenhum log.
/// </summary>
internal static class StartupLog
{
    private static readonly string? LogPath = Resolve();

    private static string? Resolve()
    {
        try
        {
            var directory = Path.Combine(
                Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
                "Compasso");

            Directory.CreateDirectory(directory);

            return Path.Combine(directory, "startup.log");
        }
        catch
        {
            return null;
        }
    }

    internal static void Write(string message)
    {
        var path = LogPath;
        if (path is null)
        {
            return;
        }

        try
        {
            File.AppendAllText(path, $"{DateTimeOffset.Now:O} {message}{Environment.NewLine}");
        }
        catch
        {
        }
    }

    internal static void Write(string message, Exception exception) => Write($"{message}: {exception}");
}
