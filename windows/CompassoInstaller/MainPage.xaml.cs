using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

// To learn more about WinUI, the WinUI project structure,
// and more about our project templates, see: http://aka.ms/winui-project-info.

namespace CompassoInstaller;

public sealed partial class MainPage : Page
{
    public MainPage()
    {
        InitializeComponent();
    }

    private async void InstallButton_Click(object sender, RoutedEventArgs e)
    {
        SetVerificationInProgress(true);
        StatusInfoBar.IsOpen = false;

        var result = await ElevationVerifier.RequestAsync();

        StatusInfoBar.Severity = result.Status switch
        {
            ElevationVerificationStatus.Confirmed => InfoBarSeverity.Success,
            ElevationVerificationStatus.Cancelled => InfoBarSeverity.Warning,
            _ => InfoBarSeverity.Error,
        };
        StatusInfoBar.Title = result.Status switch
        {
            ElevationVerificationStatus.Confirmed => "Permissão confirmada",
            ElevationVerificationStatus.Cancelled => "Permissão cancelada",
            _ => "Não foi possível continuar",
        };
        StatusInfoBar.Message = result.Message;
        StatusInfoBar.IsOpen = true;

        if (result.Status == ElevationVerificationStatus.Confirmed)
        {
            InstallButton.Content = "Verificar novamente";
        }

        SetVerificationInProgress(false);
    }

    private void CancelButton_Click(object sender, RoutedEventArgs e)
    {
        Application.Current.Exit();
    }

    private void SetVerificationInProgress(bool isInProgress)
    {
        ElevationProgress.Visibility = isInProgress ? Visibility.Visible : Visibility.Collapsed;
        ProgressText.Visibility = isInProgress ? Visibility.Visible : Visibility.Collapsed;
        InstallButton.IsEnabled = !isInProgress;
        CancelButton.IsEnabled = !isInProgress;
    }
}
