$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class CompassoInstallerNative {
    [DllImport("user32.dll")]
    public static extern bool SetForegroundWindow(IntPtr hWnd);
}
"@

$resultPath = 'C:\CompassoWinUIBootstrap\portable-installer-ui-result.txt'

function Get-InstallerWindow([int]$timeoutSeconds = 15) {
    $deadline = [DateTime]::UtcNow.AddSeconds($timeoutSeconds)
    do {
        $process = Get-Process | Where-Object {
            $_.SessionId -eq 1 -and
            $_.ProcessName -like 'CompassoSetup*' -and
            $_.MainWindowHandle -ne [IntPtr]::Zero
        } | Select-Object -First 1
        if ($process) { return $process.MainWindowHandle }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'Janela do instalador não encontrada.'
}

function Invoke-DefaultButton {
    $window = Get-InstallerWindow
    [void][CompassoInstallerNative]::SetForegroundWindow($window)
    Start-Sleep -Milliseconds 300
    [System.Windows.Forms.SendKeys]::SendWait('{ENTER}')
    Start-Sleep -Seconds 2
}

try {
    # Tarefas adicionais -> Pronto para instalar -> Concluir. Na última etapa,
    # a opção padrão abre a interface instalada.
    Invoke-DefaultButton
    Invoke-DefaultButton
    Invoke-DefaultButton

    $deadline = [DateTime]::UtcNow.AddSeconds(20)
    do {
        $app = Get-Process Compasso -ErrorAction SilentlyContinue |
            Where-Object { $_.SessionId -eq 1 -and -not $_.HasExited } |
            Select-Object -First 1
        if ($app) { break }
        Start-Sleep -Milliseconds 500
    } while ([DateTime]::UtcNow -lt $deadline)

    if (-not $app) { throw 'A interface não abriu depois da instalação.' }
    @(
        'INSTALLER_WINDOW=PASS'
        'INSTALLER_NEXT=PASS'
        'INSTALLER_INSTALL=PASS'
        'INSTALLER_FINISH=PASS'
        "APP_PROCESS_ID=$($app.Id)"
        'APP_LAUNCHED=PASS'
    ) | Set-Content -Encoding utf8 $resultPath
}
catch {
    @('INSTALLER_UI=FAIL', "ERROR=$($_.Exception.Message)") |
        Set-Content -Encoding utf8 $resultPath
    throw
}
