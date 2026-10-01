using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;

namespace CompassoApp;

/// <summary>
/// Tela inicial do aplicativo do computador. O texto, a ordem dos controles e as
/// cores seguem o conceito extraído em
/// <c>docs/design/windows-shell/README.md</c>.
/// </summary>
public sealed partial class AddTimePage : Page
{
    private readonly RadioButton[] _durationButtons;
    private int _selectedMinutes = 30;

    public AddTimePage()
    {
        InitializeComponent();

        _durationButtons = [Duration15, Duration30, Duration60, Duration120];
        SelectDuration(Duration30);
        RefreshServiceStatus();
    }

    protected override void OnNavigatedTo(NavigationEventArgs e)
    {
        base.OnNavigatedTo(e);
        StatusInfoBar.IsOpen = false;
        RefreshServiceStatus();
    }

    private void Duration_Click(object sender, RoutedEventArgs e)
    {
        if (sender is RadioButton selected)
        {
            SelectDuration(selected);
        }
    }

    private void RevealPasswordButton_Click(object sender, RoutedEventArgs e)
    {
        var reveal = PasswordBox.PasswordRevealMode != PasswordRevealMode.Visible;
        PasswordBox.PasswordRevealMode = reveal
            ? PasswordRevealMode.Visible
            : PasswordRevealMode.Hidden;
        AutomationProperties.SetName(
            RevealPasswordButton,
            reveal ? "Ocultar a senha" : "Mostrar a senha");
    }

    /// <summary>Aplica coral apenas ao período escolhido.</summary>
    private void SelectDuration(RadioButton selected)
    {
        foreach (var button in _durationButtons)
        {
            var isSelected = ReferenceEquals(button, selected);
            button.IsChecked = isSelected;
            AutomationProperties.SetHelpText(button, isSelected ? "Selecionado" : "Não selecionado");
        }

        _selectedMinutes = int.TryParse(selected.Tag?.ToString(), out var minutes) ? minutes : 30;
        UpdateConfirmButton();
    }

    private void UpdateConfirmButton()
    {
        AddTimeButton.Content = $"Adicionar {_selectedMinutes} minutos";
    }

    private void RefreshServiceStatus()
    {
        // The service is not part of the current payload yet. Keep the chip
        // honest instead of showing a successful connection state.
        ConnectionStatusText.Text = "Serviço não instalado";
    }

    private void AddTimeButton_Click(object sender, RoutedEventArgs e)
    {
        if (string.IsNullOrEmpty(PasswordBox.Password))
        {
            ShowStatus(InfoBarSeverity.Warning, "Informe a senha", "Digite a senha do responsável para confirmar.");
            return;
        }

        // A adicao de tempo depende do servico, que ainda nao existe. Dizer que
        // foi adicionado seria falso, entao a tela informa o que falta.
        ShowStatus(
            InfoBarSeverity.Informational,
            "Ainda não é possível adicionar",
            "O serviço do Compasso ainda não está instalado neste computador. Use Configurações para conectar ao painel.");
    }

    private void OpenSettingsButton_Click(object sender, RoutedEventArgs e)
    {
        if (Frame.Navigate(typeof(SettingsPage)))
        {
            return;
        }

        ShowStatus(InfoBarSeverity.Error, "Não foi possível abrir", "A tela de configurações não pôde ser carregada.");
    }

    private void ShowStatus(InfoBarSeverity severity, string title, string message)
    {
        StatusInfoBar.Severity = severity;
        StatusInfoBar.Title = title;
        StatusInfoBar.Message = message;
        StatusInfoBar.IsOpen = true;
    }
}
