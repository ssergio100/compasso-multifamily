using System.Runtime.InteropServices;
using System.Text;
using System.Diagnostics;
using Microsoft.Win32;

namespace CompassoInstaller;

/// <summary>
/// Aplica um <see cref="InstallPlan"/> no sistema. Roda exclusivamente dentro do
/// processo elevado.
///
/// O plano é declarativo de propósito: a interface não elevada decide o que
/// será gravado, o trabalhador elevado decide apenas como gravar. Assim o
/// privilégio administrativo nunca fica do lado de quem está desenhando a tela,
/// e o motor pode ser exercitado com qualquer payload durante o
/// desenvolvimento, antes de as interfaces existirem.
/// </summary>
internal sealed class InstallPlanExecutor
{
    private const string AppUserModelId = "Compasso.Multifamily.Desktop";
    private readonly InstallPlan _plan;
    private readonly string _payloadDirectory;
    private readonly string _stagingDirectory;
    private readonly string _backupDirectory;

    internal InstallPlanExecutor(InstallPlan plan, string payloadDirectory)
    {
        _plan = plan;
        _payloadDirectory = payloadDirectory;
        _stagingDirectory = plan.InstallDirectory + ".new";
        _backupDirectory = plan.InstallDirectory + ".previous";
    }

    internal async Task ApplyAsync(Func<string, int, Task> report, CancellationToken cancellationToken)
    {
        var steps = BuildSteps();
        var completed = 0;

        foreach (var step in steps)
        {
            cancellationToken.ThrowIfCancellationRequested();

            var percent = (int)(100.0 * completed / steps.Count);
            await report(step.Label, percent);

            step.Action();
            completed++;

            await report(step.Label, (int)(100.0 * completed / steps.Count));
        }
    }

    private List<(string Label, Action Action)> BuildSteps()
    {
        var steps = new List<(string, Action)>();

        steps.Add(("Fechando o Compasso para atualizar", CloseRunningApplication));
        steps.Add(($"Preparando {_plan.InstallDirectory}", PrepareStagingDirectory));

        foreach (var file in _plan.Files)
        {
            var file_ = file;
            steps.Add(($"Copiando {Path.GetFileName(file_.Destination)}", () => CopyFile(file_)));
        }

        if (PortableSetup.IsAvailable)
        {
            steps.Add(("Instalando o componente de manutenção", () =>
                PortableSetup.CopyToStaging(_stagingDirectory)));
        }

        steps.Add(("Ativando a nova versão", CommitStagedInstallation));
        steps.Add(("Atualizando o atalho da área de trabalho", RemoveDesktopShortcut));

        foreach (var shortcut in _plan.Shortcuts)
        {
            var shortcut_ = shortcut;
            steps.Add(($"Criando atalho {Path.GetFileName(shortcut_.ShortcutPath)}", () => CreateShortcut(shortcut_)));
        }

        if (!string.IsNullOrWhiteSpace(_plan.Uninstall.KeyPath))
        {
            steps.Add(("Registrando a desinstalação", WriteUninstallEntry));
        }

        steps.Add(("Atualizando o menu Iniciar", RefreshWindowsShell));

        return steps;
    }

    private void PrepareStagingDirectory()
    {
        if (string.IsNullOrWhiteSpace(_plan.InstallDirectory))
        {
            throw new InvalidOperationException("O plano não informa onde instalar.");
        }

        DeleteDirectoryIfExists(_stagingDirectory);
        DeleteDirectoryIfExists(_backupDirectory);
        Directory.CreateDirectory(_stagingDirectory);
    }

    private void CloseRunningApplication()
    {
        foreach (var process in Process.GetProcessesByName("CompassoApp"))
        {
            try
            {
                process.CloseMainWindow();
                if (!process.WaitForExit(5000))
                {
                    // Atualizações elevadas podem rodar em outra sessão e não
                    // conseguir enviar WM_CLOSE à janela interativa. Encerrar
                    // somente o processo do Compasso torna a atualização
                    // previsível e evita pedir desinstalação manual.
                    process.Kill(entireProcessTree: true);
                    process.WaitForExit(5000);
                }
            }
            finally
            {
                process.Dispose();
            }
        }
    }

    private void CopyFile(PayloadFile file)
    {
        if (string.IsNullOrWhiteSpace(file.Source) || string.IsNullOrWhiteSpace(file.Destination))
        {
            throw new InvalidOperationException("O plano contém um arquivo sem origem ou sem destino.");
        }

        var source = Path.IsPathRooted(file.Source)
            ? file.Source
            : Path.Combine(_payloadDirectory, file.Source);

        if (!File.Exists(source))
        {
            throw new FileNotFoundException($"Componente ausente no pacote: {file.Source}", source);
        }

        var destination = Path.IsPathRooted(file.Destination)
            ? file.Destination
            : Path.Combine(_stagingDirectory, file.Destination);

        var parent = Path.GetDirectoryName(destination);
        if (!string.IsNullOrEmpty(parent))
        {
            Directory.CreateDirectory(parent);
        }

        // Um instalador que falha no meio deixa um executável truncado em disco.
        // Copiar para um temporário e mover por cima mantém o destino íntegro.
        var staging = destination + ".compasso-staging";
        File.Copy(source, staging, overwrite: true);
        File.Move(staging, destination, overwrite: true);
    }

    private void CommitStagedInstallation()
    {
        var hadPreviousInstallation = Directory.Exists(_plan.InstallDirectory);
        try
        {
            if (hadPreviousInstallation)
            {
                Directory.Move(_plan.InstallDirectory, _backupDirectory);
            }

            Directory.Move(_stagingDirectory, _plan.InstallDirectory);
            DeleteDirectoryIfExists(_backupDirectory);
        }
        catch
        {
            if (!Directory.Exists(_plan.InstallDirectory) && Directory.Exists(_backupDirectory))
            {
                Directory.Move(_backupDirectory, _plan.InstallDirectory);
            }

            throw;
        }
    }

    private static void DeleteDirectoryIfExists(string path)
    {
        if (Directory.Exists(path))
        {
            Directory.Delete(path, recursive: true);
        }
    }

    private static void RemoveDesktopShortcut()
    {
        if (File.Exists(InstallPaths.DesktopShortcutPath))
        {
            File.Delete(InstallPaths.DesktopShortcutPath);
        }
    }

    private static void CreateShortcut(PayloadShortcut shortcut)
    {
        if (string.IsNullOrWhiteSpace(shortcut.Target) || string.IsNullOrWhiteSpace(shortcut.ShortcutPath))
        {
            throw new InvalidOperationException("O plano contém um atalho sem alvo ou sem caminho.");
        }

        var parent = Path.GetDirectoryName(shortcut.ShortcutPath);
        if (!string.IsNullOrEmpty(parent))
        {
            Directory.CreateDirectory(parent);
        }

        var link = (IShellLinkW)new ShellLink();
        link.SetPath(shortcut.Target);
        link.SetWorkingDirectory(Path.GetDirectoryName(shortcut.Target) ?? string.Empty);

        if (!string.IsNullOrWhiteSpace(shortcut.Description))
        {
            link.SetDescription(shortcut.Description);
        }

        if (!string.IsNullOrWhiteSpace(shortcut.IconLocation))
        {
            link.SetIconLocation(shortcut.IconLocation, 0);
        }

        ((IPersistFile)link).Save(shortcut.ShortcutPath, fRemember: true);

        // Match the unpackaged process identity so Start/Search and the taskbar
        // resolve the installed shortcut and the running window as one app.
        SetShortcutAppIdentity(shortcut);
    }

    private static void SetShortcutAppIdentity(PayloadShortcut shortcut)
    {
        var propertyStore = (IPropertyStore)ShellPropertyStore.FromPath(shortcut.ShortcutPath);
        try
        {
            // Relaunch metadata comes first; setting the AppUserModelID last
            // tells the taskbar to refresh the complete identity.
            SetStringProperty(propertyStore, 2, shortcut.Target);
            if (!string.IsNullOrWhiteSpace(shortcut.IconLocation))
            {
                SetStringProperty(propertyStore, 3, shortcut.IconLocation + ",0");
            }
            SetStringProperty(propertyStore, 5, AppUserModelId);
            Marshal.ThrowExceptionForHR(propertyStore.Commit());
        }
        finally
        {
            Marshal.ReleaseComObject(propertyStore);
        }
    }

    private static void SetStringProperty(IPropertyStore propertyStore, uint propertyId, string text)
    {
        var key = new PropertyKey(new Guid("9F4C2855-9F79-4B39-A8D0-E1D42DE1D5F3"), propertyId);
        var pointer = Marshal.StringToCoTaskMemUni(text);
        var value = new PropVariant { VariantType = 31, PointerValue = pointer };
        try
        {
            Marshal.ThrowExceptionForHR(propertyStore.SetValue(ref key, ref value));
        }
        finally
        {
            PropVariantClear(ref value);
        }
    }

    [DllImport("shell32.dll", CharSet = CharSet.Unicode, PreserveSig = true)]
    private static extern int SHGetPropertyStoreFromParsingName(
        string path, IntPtr bindContext, uint flags, ref Guid interfaceId,
        [MarshalAs(UnmanagedType.Interface)] out IPropertyStore propertyStore);

    [DllImport("ole32.dll")]
    private static extern int PropVariantClear(ref PropVariant value);

    private static class ShellPropertyStore
    {
        internal static IPropertyStore FromPath(string path)
        {
            var iid = typeof(IPropertyStore).GUID;
            Marshal.ThrowExceptionForHR(SHGetPropertyStoreFromParsingName(path, IntPtr.Zero, 2, ref iid, out var store));
            return store;
        }
    }

    private void WriteUninstallEntry()
    {
        var entry = _plan.Uninstall;

        using var key = RegistryKey.OpenBaseKey(
            RegistryHive.LocalMachine,
            RegistryView.Registry64).CreateSubKey(entry.KeyPath, writable: true)
            ?? throw new InvalidOperationException("Não foi possível abrir a chave de desinstalação.");

        key.SetValue("DisplayName", entry.DisplayName, RegistryValueKind.String);
        key.SetValue("DisplayVersion", entry.DisplayVersion, RegistryValueKind.String);
        key.SetValue("Publisher", entry.Publisher, RegistryValueKind.String);
        key.SetValue("UninstallString", entry.UninstallString, RegistryValueKind.String);
        key.SetValue("DisplayIcon", Path.Combine(_plan.InstallDirectory, "Assets", "AppIcon.ico") + ",0", RegistryValueKind.String);
        key.SetValue("InstallLocation", _plan.InstallDirectory, RegistryValueKind.String);
        key.SetValue("NoModify", entry.NoModify, RegistryValueKind.DWord);
        key.SetValue("NoRepair", entry.NoRepair, RegistryValueKind.DWord);
    }

    private static void RefreshWindowsShell()
    {
        const uint AssociationChanged = 0x08000000;
        const uint FlushNoWait = 0x00002000;
        SHChangeNotify(AssociationChanged, FlushNoWait, IntPtr.Zero, IntPtr.Zero);
    }

    [DllImport("shell32.dll")]
    private static extern void SHChangeNotify(uint eventId, uint flags, IntPtr item1, IntPtr item2);
}

[StructLayout(LayoutKind.Sequential, Pack = 4)]
internal struct PropertyKey(Guid formatId, uint propertyId)
{
    public Guid FormatId = formatId;
    public uint PropertyId = propertyId;
}

[StructLayout(LayoutKind.Explicit)]
internal struct PropVariant
{
    [FieldOffset(0)] public ushort VariantType;
    [FieldOffset(8)] public IntPtr PointerValue;
}

[ComImport]
[Guid("886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99")]
[InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
internal interface IPropertyStore
{
    [PreserveSig] int GetCount(out uint count);
    [PreserveSig] int GetAt(uint index, out PropertyKey key);
    [PreserveSig] int GetValue(ref PropertyKey key, out PropVariant value);
    [PreserveSig] int SetValue(ref PropertyKey key, ref PropVariant value);
    [PreserveSig] int Commit();
}

/// <summary>
/// Caminhos de destino, em um só lugar. Fixados no log de decisões porque são a
/// parte do plano que precisa sobreviver a qualquer payload futuro.
/// </summary>
internal static class InstallPaths
{
    internal const string InstallDirectory = @"C:\Program Files\Compasso";

    internal const string InstalledSetupPath =
        @"C:\Program Files\Compasso\Installer\CompassoSetup.exe";

    internal const string StartMenuDirectory =
        @"C:\ProgramData\Microsoft\Windows\Start Menu\Programs\Compasso";

    internal const string UninstallKeyPath =
        @"SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Compasso";

    internal const string DesktopShortcutPath = @"C:\Users\Public\Desktop\Compasso.lnk";
}

[ComImport]
[Guid("00021401-0000-0000-C000-000000000046")]
internal class ShellLink
{
}

[ComImport]
[InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
[Guid("000214F9-0000-0000-C000-000000000046")]
internal interface IShellLinkW
{
    void GetPath([Out][MarshalAs(UnmanagedType.LPWStr)] StringBuilder pszFile, int cch, IntPtr pfd, uint fFlags);

    void GetIDList(out IntPtr ppidl);

    void SetIDList(IntPtr pidl);

    void GetDescription([Out][MarshalAs(UnmanagedType.LPWStr)] StringBuilder pszName, int cch);

    void SetDescription([MarshalAs(UnmanagedType.LPWStr)] string pszName);

    void GetWorkingDirectory([Out][MarshalAs(UnmanagedType.LPWStr)] StringBuilder pszDir, int cch);

    void SetWorkingDirectory([MarshalAs(UnmanagedType.LPWStr)] string pszDir);

    void GetArguments([Out][MarshalAs(UnmanagedType.LPWStr)] StringBuilder pszArgs, int cch);

    void SetArguments([MarshalAs(UnmanagedType.LPWStr)] string pszArgs);

    void GetHotkey(out short pwHotkey);

    void SetHotkey(short wHotkey);

    void GetShowCmd(out int piShowCmd);

    void SetShowCmd(int iShowCmd);

    void GetIconLocation([Out][MarshalAs(UnmanagedType.LPWStr)] StringBuilder pszIconPath, int cch, out int piIcon);

    void SetIconLocation([MarshalAs(UnmanagedType.LPWStr)] string pszIconPath, int iIcon);

    void SetRelativePath([MarshalAs(UnmanagedType.LPWStr)] string pszPathRel, uint dwReserved);

    void Resolve(IntPtr hwnd, uint fFlags);

    void SetPath([MarshalAs(UnmanagedType.LPWStr)] string pszFile);
}

[ComImport]
[InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
[Guid("0000010b-0000-0000-C000-000000000046")]
internal interface IPersistFile
{
    void GetClassID(out Guid pClassID);

    [PreserveSig]
    int IsDirty();

    void Load([MarshalAs(UnmanagedType.LPWStr)] string pszFileName, uint dwMode);

    void Save([MarshalAs(UnmanagedType.LPWStr)] string pszFileName, [MarshalAs(UnmanagedType.Bool)] bool fRemember);

    void SaveCompleted([MarshalAs(UnmanagedType.LPWStr)] string pszFileName);

    void GetCurFile([MarshalAs(UnmanagedType.LPWStr)] out string ppszFileName);
}
