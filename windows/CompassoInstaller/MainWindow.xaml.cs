using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Navigation;
using Windows.Graphics;

// To learn more about WinUI, the WinUI project structure,
// and more about our project templates, see: http://aka.ms/winui-project-info.

namespace CompassoInstaller;

public sealed partial class MainWindow : Window
{
    public MainWindow()
    {
        InitializeComponent();

        ExtendsContentIntoTitleBar = true;
        SetTitleBar(AppTitleBar);

        AppWindow.SetIcon("Assets/AppIcon.ico");
        AppWindow.Resize(new SizeInt32(1180, 720));

        RootFrame.NavigationFailed += RootFrame_NavigationFailed;
        if (!RootFrame.Navigate(typeof(MainPage)))
        {
            throw new InvalidOperationException("Não foi possível abrir a página inicial do instalador.");
        }
    }

    private static void RootFrame_NavigationFailed(object sender, NavigationFailedEventArgs e)
    {
#if DEBUG
        var path = System.IO.Path.Combine(AppContext.BaseDirectory, "startup.log");
        File.AppendAllText(path, $"{DateTimeOffset.Now:O} Main page navigation failed: {e.Exception}{Environment.NewLine}");
#endif
        throw new InvalidOperationException("Falha ao carregar a página inicial do instalador.", e.Exception);
    }
}
