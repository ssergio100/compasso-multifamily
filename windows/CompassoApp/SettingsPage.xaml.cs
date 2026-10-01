using System.Collections.Generic;
using System.Runtime.InteropServices;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;

namespace CompassoApp;

/// <summary>
/// Configuração do computador. O texto, a ordem dos campos e as cores seguem o
/// conceito extraído em <c>docs/design/windows-shell/README.md</c>.
/// </summary>
public sealed partial class SettingsPage : Page
{
    public SettingsPage()
    {
        InitializeComponent();

        LoadLocalAccounts();
    }

    /// <summary>
    /// Preenche o seletor com as contas locais do Windows. Usa
    /// <c>NetUserEnum</c> para listar as contas de verdade, em vez de um nome
    /// fixo, e cai na conta atual se a API nao responder.
    /// </summary>
    private void LoadLocalAccounts()
    {
        var accounts = new List<string>(EnumerateLocalAccounts());

        var current = Environment.UserName;
        if (!accounts.Contains(current, StringComparer.OrdinalIgnoreCase))
        {
            accounts.Insert(0, current);
        }

        foreach (var account in accounts)
        {
            AccountBox.Items.Add(account);
        }

        if (AccountBox.Items.Count > 0)
        {
            AccountBox.SelectedIndex = 0;
        }
    }

    private static IEnumerable<string> EnumerateLocalAccounts()
    {
        const int FilterNormalAccount = 0x0002;
        const int PreferredMaximumLength = -1;

        if (NetUserEnum(null, 0, FilterNormalAccount, out var buffer, PreferredMaximumLength,
                out var entriesRead, out _, out _) != 0
            || buffer == IntPtr.Zero
            || entriesRead <= 0)
        {
            yield break;
        }

        try
        {
            var stride = Marshal.SizeOf<USER_INFO_1>();
            for (var i = 0; i < entriesRead; i++)
            {
                var entry = IntPtr.Add(buffer, i * stride);
                var name = Marshal.PtrToStringUni(Marshal.ReadIntPtr(entry, IntPtr.Size));
                if (!string.IsNullOrWhiteSpace(name))
                {
                    yield return name;
                }
            }
        }
        finally
        {
            NetApiBufferFree(buffer);
        }
    }

    private void RevealTokenButton_Click(object sender, RoutedEventArgs e)
    {
        var revealing = TokenBox.PasswordRevealMode == PasswordRevealMode.Visible;
        TokenBox.PasswordRevealMode = revealing
            ? PasswordRevealMode.Hidden
            : PasswordRevealMode.Visible;

        RevealTokenButton.Content = revealing ? "Mostrar" : "Ocultar";
        AutomationProperties.SetName(
            RevealTokenButton,
            revealing ? "Mostrar token" : "Ocultar token");
    }

    private void SaveButton_Click(object sender, RoutedEventArgs e)
    {
        if (AccountBox.SelectedItem is null)
        {
            ShowStatus(
                InfoBarSeverity.Warning,
                "Escolha a conta",
                "Selecione a conta do Windows que o Compasso vai acompanhar.");
            return;
        }

        if (!ConsentCheckBox.IsChecked is true)
        {
            ShowStatus(
                InfoBarSeverity.Warning,
                "Falta a confirmação",
                "Confirme que esta conta poderá ter a sessão bloqueada pelo Compasso.");
            return;
        }

        if (string.IsNullOrWhiteSpace(ServerBox.Text)
            || string.IsNullOrWhiteSpace(DeviceIdBox.Text)
            || string.IsNullOrWhiteSpace(TokenBox.Password))
        {
            ShowStatus(
                InfoBarSeverity.Warning,
                "Faltam dados",
                "Preencha o identificador do dispositivo e o token do painel.");
            return;
        }

        // O armazenamento das credenciais depende do serviço, que ainda nao existe.
        // Confirmar a conexao aqui seria mentir sobre o estado real do computador.
        ShowStatus(
            InfoBarSeverity.Informational,
            "Ainda não é possível conectar",
            "O serviço do Compasso ainda não está instalado neste computador. Reabra o instalador para concluí-lo.");
    }

    private void BackButton_Click(object sender, RoutedEventArgs e)
    {
        if (Frame.Navigate(typeof(AddTimePage)))
        {
            return;
        }

        ShowStatus(InfoBarSeverity.Error, "Não foi possível voltar", "A tela de adicionar tempo não pôde ser carregada.");
    }

    private void ShowStatus(InfoBarSeverity severity, string title, string message)
    {
        StatusInfoBar.Severity = severity;
        StatusInfoBar.Title = title;
        StatusInfoBar.Message = message;
        StatusInfoBar.IsOpen = true;
    }

    [DllImport("netapi32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern int NetUserEnum(
        string? serverName,
        int level,
        int filter,
        out IntPtr buffer,
        int preferredMaximumLength,
        out int entriesRead,
        out int totalEntriesRead,
        out int resumeHandle);

    [DllImport("netapi32.dll")]
    private static extern int NetApiBufferFree(IntPtr buffer);

    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    private struct USER_INFO_1
    {
        public string? name;
        public string? password;
        public int passwordAge;
        public int priv;
        public string? homeDir;
        public string? comment;
        public int flags;
        public string? scriptPath;
    }
}
