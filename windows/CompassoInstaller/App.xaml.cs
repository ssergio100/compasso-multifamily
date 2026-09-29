using Windows.ApplicationModel;
using Windows.ApplicationModel.Activation;
using Windows.Foundation;
using Windows.Foundation.Collections;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Controls.Primitives;
using Microsoft.UI.Xaml.Data;
using Microsoft.UI.Xaml.Input;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Navigation;
using Microsoft.UI.Xaml.Shapes;

// To learn more about WinUI, the WinUI project structure,
// and more about our project templates, see: http://aka.ms/winui-project-info.

namespace CompassoInstaller;

/// <summary>
/// Provides application-specific behavior to supplement the default Application class.
/// </summary>
public partial class App : Application
{
    private Window? _window;

#if DEBUG
    private static readonly string StartupLogPath = System.IO.Path.Combine(AppContext.BaseDirectory, "startup.log");
#endif
    
    /// <summary>
    /// Initializes the singleton application object.  This is the first line of authored code
    /// executed, and as such is the logical equivalent of main() or WinMain().
    /// </summary>
    public App()
    {
#if DEBUG
        File.AppendAllText(StartupLogPath, $"{DateTimeOffset.Now:O} App constructor started.{Environment.NewLine}");
#endif
        InitializeComponent();
#if DEBUG
        File.AppendAllText(StartupLogPath, $"{DateTimeOffset.Now:O} App resources initialized.{Environment.NewLine}");
#endif
    }

    /// <summary>
    /// Invoked when the application is launched.
    /// </summary>
    /// <param name="args">Details about the launch request and process.</param>
    protected override async void OnLaunched(Microsoft.UI.Xaml.LaunchActivatedEventArgs args)
    {
        try
        {
#if DEBUG
            File.AppendAllText(StartupLogPath, $"{DateTimeOffset.Now:O} Launch received.{Environment.NewLine}");
#endif
            if (ElevationVerifier.TryGetProbePipeName(Environment.GetCommandLineArgs(), out var pipeName))
            {
                Environment.ExitCode = await ElevationVerifier.RunProbeAsync(pipeName);
                Exit();
                return;
            }

            _window = new MainWindow();
#if DEBUG
            File.AppendAllText(StartupLogPath, $"{DateTimeOffset.Now:O} Main window constructed.{Environment.NewLine}");
#endif
            _window.Activate();
#if DEBUG
            File.AppendAllText(StartupLogPath, $"{DateTimeOffset.Now:O} Main window activated.{Environment.NewLine}");
#endif
        }
        catch (Exception exception)
        {
#if DEBUG
            File.AppendAllText(StartupLogPath, $"{DateTimeOffset.Now:O} Startup failed: {exception}{Environment.NewLine}");
#endif
            throw;
        }
    }
}
