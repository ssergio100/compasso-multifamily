using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;
using Windows.Graphics;

namespace CompassoApp;

public sealed partial class MainWindow : Window
{
    // Dimensoes derivadas da area visivel dos conceitos em docs/design (PNG a
    // aproximadamente 2x): Adicionar tempo 504x634 e Configuracoes 530x708.
    private const int AddTimeWidth = 504;
    private const int SettingsWidth = 530;
    private const int AddTimeHeight = 634;
    private const int SettingsHeight = 708;

    public MainWindow()
    {
        InitializeComponent();

        ExtendsContentIntoTitleBar = true;
        SetTitleBar(AppTitleBar);
        var appIconPath = Path.Combine(AppContext.BaseDirectory, "Assets", "AppIcon.ico");
        AppWindow.SetIcon(appIconPath);
        AppWindow.SetTaskbarIcon(appIconPath);

        AppWindow.Resize(new SizeInt32(AddTimeWidth, AddTimeHeight));

        RootFrame.NavigationFailed += RootFrame_NavigationFailed;
        RootFrame.Navigated += RootFrame_Navigated;
        if (!RootFrame.Navigate(typeof(AddTimePage)))
        {
            throw new InvalidOperationException("Não foi possível abrir a tela inicial do Compasso.");
        }
    }

    /// <summary>
    /// Ajusta a dimensao da janela a cada tela conforme a proporcao dos PNG de
    /// referencia. Configuracoes e ligeiramente mais larga e alta para acomodar
    /// seus campos sem comprimir a composicao vertical do conceito.
    /// </summary>
    private void RootFrame_Navigated(object sender, NavigationEventArgs e)
    {
        var isSettings = e.SourcePageType == typeof(SettingsPage);
        var width = isSettings ? SettingsWidth : AddTimeWidth;
        var height = isSettings ? SettingsHeight : AddTimeHeight;
        var size = AppWindow.Size;
        if (size.Width != width || size.Height != height)
        {
            AppWindow.Resize(new SizeInt32(width, height));
        }
    }

    private static void RootFrame_NavigationFailed(object sender, NavigationFailedEventArgs e)
    {
#if DEBUG
        StartupLog.Write("Navigation failed", e.Exception);
#endif
        throw new InvalidOperationException("Falha ao carregar uma tela do aplicativo.", e.Exception);
    }
}
