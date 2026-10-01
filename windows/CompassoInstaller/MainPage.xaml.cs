using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.Win32;

// To learn about WinUI, the WinUI project structure,
// and more about your project templates, see: http://aka.ms/winui-project-info.

namespace CompassoInstaller;

public sealed partial class MainPage : Page
{
    private readonly DispatcherQueue _dispatcher;
    private ElevatedSession? _session;
    private bool _operationInProgress;

    public MainPage()
    {
        InitializeComponent();
        _dispatcher = DispatcherQueue.GetForCurrentThread();
        DesktopShortcutCheckBox.IsChecked = File.Exists(InstallPaths.DesktopShortcutPath);
        RefreshInstalledState();
    }

    /// <summary>
    /// Decide entre instalar e desinstalar. A chave de registro é a fonte da
    /// verdade porque é ela que o Windows lê em "Apps e recursos instalados"; o
    /// diretório sozinho pode sobrar de uma instalação interrompida.
    /// </summary>
    private void RefreshInstalledState()
    {
        var installed = IsInstalled();

        InstallButton.Visibility = Visibility.Visible;
        InstallButton.Content = installed ? "Atualizar ou reparar" : "Instalar";
        PageTitle.Text = installed ? "Atualizar o Compasso" : "Instalar o Compasso";
        UninstallButton.Visibility = installed ? Visibility.Visible : Visibility.Collapsed;
    }

    private static bool IsInstalled()
    {
        using var hive = RegistryKey.OpenBaseKey(RegistryHive.LocalMachine, RegistryView.Registry64);
        using var key = hive.OpenSubKey(InstallPaths.UninstallKeyPath);
        return key is not null;
    }

    private async void InstallButton_Click(object sender, RoutedEventArgs e)
    {
        if (_operationInProgress)
        {
            return;
        }

        _operationInProgress = true;

        SetBusy(true, "Solicitando permissão de administrador…");
        StatusInfoBar.IsOpen = false;

        if (!await EnsureElevatedAsync())
        {
            _operationInProgress = false;
            return;
        }

        // O elevate confirmou o privilégio, mas ainda não instalou nada. Dizer o
        // contrário aqui seria mentir para quem está usando.
        var plan = InstallPlanFactory.TryCreatePlan(AppContext.BaseDirectory);
        if (plan is null)
        {
            ProgressText.Visibility = Visibility.Visible;
            ProgressText.Text = "Nenhum componente no pacote para instalar ainda.";
            SetBusy(false, null);
            _operationInProgress = false;
            return;
        }

        InstallPlanFactory.SetDesktopShortcut(plan, DesktopShortcutCheckBox.IsChecked is true);

        ProgressText.Visibility = Visibility.Visible;
        ElevationProgress.IsIndeterminate = false;

        try
        {
            var result = await _session!.InstallAsync(plan, CancellationToken.None);
            ElevationProgress.Value = Math.Clamp(result.Percent, 0, 100);

            ShowStatus(
                result.Succeeded ? InfoBarSeverity.Success : InfoBarSeverity.Error,
                result.Succeeded ? "Instalação concluída" : "A instalação falhou",
                result.Text);

            if (result.Succeeded)
            {
                RefreshInstalledState();
            }
        }
        catch (Exception exception)
        {
            ShowStatus(InfoBarSeverity.Error, "A instalação falhou", exception.Message);
        }
        finally
        {
            _operationInProgress = false;
            SetBusy(false, null);
        }
    }

    private async void UninstallButton_Click(object sender, RoutedEventArgs e)
    {
        if (_operationInProgress)
        {
            return;
        }

        _operationInProgress = true;

        SetBusy(true, "Solicitando permissão de administrador…");
        StatusInfoBar.IsOpen = false;

        if (!await EnsureElevatedAsync())
        {
            _operationInProgress = false;
            return;
        }

        var plan = InstallPlanFactory.TryCreatePlan(AppContext.BaseDirectory);
        if (plan is null)
        {
            ShowStatus(
                InfoBarSeverity.Error,
                "Pacote incompleto",
                "O instalador precisa do payload.json para saber o que remover.");
            SetBusy(false, null);
            _operationInProgress = false;
            return;
        }

        ProgressText.Visibility = Visibility.Visible;
        ElevationProgress.IsIndeterminate = false;

        try
        {
            var result = await _session!.UninstallAsync(plan, CancellationToken.None);
            ElevationProgress.Value = Math.Clamp(result.Percent, 0, 100);

            ShowStatus(
                result.Succeeded ? InfoBarSeverity.Success : InfoBarSeverity.Error,
                result.Succeeded ? "Desinstalação concluída" : "A desinstalação falhou",
                result.Text);

            if (result.Succeeded)
            {
                RefreshInstalledState();
            }
        }
        catch (Exception exception)
        {
            ShowStatus(InfoBarSeverity.Error, "A desinstalação falhou", exception.Message);
        }
        finally
        {
            _operationInProgress = false;
            SetBusy(false, null);
        }
    }

    /// <summary>
    /// Abre o canal com o processo elevado, se ainda não houver um. Devolve
    /// <c>false</c> quando a permissão foi negada, e a interface já foi avisada.
    /// </summary>
    private async Task<bool> EnsureElevatedAsync()
    {
        if (_session is not null)
        {
            return true;
        }

        var elevation = await InstallEngine.ConnectAsync();

        if (elevation.Session is null)
        {
            ShowStatus(
                elevation.Status == ElevateStatus.Cancelled
                    ? InfoBarSeverity.Warning
                    : InfoBarSeverity.Error,
                elevation.Status == ElevateStatus.Cancelled
                    ? "Permissão cancelada"
                    : "Não foi possível continuar",
                elevation.Message);
            SetBusy(false, null);
            return false;
        }

        _session = elevation.Session;
        _session.Notified += OnWorkerNotified;

        ShowStatus(
            InfoBarSeverity.Success,
            elevation.Status == ElevateStatus.AlreadyElevated
                ? "Permissão já ativa"
                : "Permissão confirmada",
            elevation.Message);

        return true;
    }

    private void OnWorkerNotified(object? sender, WorkerMessage message)
    {
        if (string.Equals(message.Type, MessageType.Progress, StringComparison.Ordinal))
        {
            _dispatcher.TryEnqueue(() =>
            {
                ProgressText.Text = message.Text;
                ElevationProgress.Value = Math.Clamp(message.Percent, 0, 100);
            });
        }
    }

    private async void CancelButton_Click(object sender, RoutedEventArgs e)
    {
        await ShutdownSessionAsync();
        Application.Current.Exit();
    }

    internal async Task ShutdownSessionAsync()
    {
        if (_session is not null)
        {
            _session.Notified -= OnWorkerNotified;
            await _session.ShutdownAsync();
            await _session.DisposeAsync();
            _session = null;
        }
    }

    private void ShowStatus(InfoBarSeverity severity, string title, string message)
    {
        StatusInfoBar.Severity = severity;
        StatusInfoBar.Title = title;
        StatusInfoBar.Message = message;
        StatusInfoBar.IsOpen = true;
    }

    private void SetBusy(bool isBusy, string? progressText)
    {
        ElevationProgress.Visibility = isBusy ? Visibility.Visible : Visibility.Collapsed;
        ProgressText.Visibility = isBusy ? Visibility.Visible : Visibility.Collapsed;
        InstallButton.IsEnabled = !isBusy;
        UninstallButton.IsEnabled = !isBusy;
        CancelButton.IsEnabled = !isBusy;
        DesktopShortcutCheckBox.IsEnabled = !isBusy;

        if (isBusy)
        {
            ElevationProgress.IsIndeterminate = true;
            ElevationProgress.Value = 0;
            ProgressText.Text = progressText ?? string.Empty;
        }
    }
}
