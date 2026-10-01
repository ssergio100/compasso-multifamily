<# Valida desinstalar -> reinstalar -> fechar na mesma sessão elevada. #>
[CmdletBinding()]
param(
    [string]$Installer = 'C:\CompassoWinUIBootstrap\unpackaged\CompassoInstaller\bin\x64\Debug\net10.0-windows10.0.26100.0\win-x64\CompassoInstaller.exe',
    [string]$ResultPath = 'C:\CompassoWinUIBootstrap\installer-cycle-result.txt'
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes

function Find-Window([int]$processId, [int]$timeoutSeconds = 15) {
    $deadline = [DateTime]::UtcNow.AddSeconds($timeoutSeconds)
    $condition = [System.Windows.Automation.PropertyCondition]::new(
        [System.Windows.Automation.AutomationElement]::ProcessIdProperty, $processId)
    do {
        $window = [System.Windows.Automation.AutomationElement]::RootElement.FindFirst(
            [System.Windows.Automation.TreeScope]::Children, $condition)
        if ($null -ne $window) { return $window }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'A janela do instalador não apareceu.'
}

function Find-InstallerWindow($launcher, [int]$timeoutSeconds = 30) {
    $deadline = [DateTime]::UtcNow.AddSeconds($timeoutSeconds)
    do {
        $candidateIds = [System.Collections.Generic.List[int]]::new()
        if (-not $launcher.HasExited) { $candidateIds.Add($launcher.Id) }
        Get-Process CompassoInstaller -ErrorAction SilentlyContinue |
            Where-Object { $_.StartTime -ge $launcher.StartTime.AddSeconds(-1) } |
            ForEach-Object { $candidateIds.Add($_.Id) }

        foreach ($candidateId in $candidateIds) {
            $condition = [System.Windows.Automation.PropertyCondition]::new(
                [System.Windows.Automation.AutomationElement]::ProcessIdProperty, $candidateId)
            $window = [System.Windows.Automation.AutomationElement]::RootElement.FindFirst(
                [System.Windows.Automation.TreeScope]::Children, $condition)
            if ($null -ne $window) {
                return [pscustomobject]@{
                    Window = $window
                    Process = Get-Process -Id $candidateId
                }
            }
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'A janela do instalador não apareceu.'
}

function Find-Control($root, [string]$id) {
    $condition = [System.Windows.Automation.PropertyCondition]::new(
        [System.Windows.Automation.AutomationElement]::AutomationIdProperty, $id)
    return $root.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $condition)
}

function Wait-Control($root, [string]$id, [int]$timeoutSeconds = 60) {
    $deadline = [DateTime]::UtcNow.AddSeconds($timeoutSeconds)
    do {
        $control = Find-Control $root $id
        if ($null -ne $control -and -not $control.Current.IsOffscreen -and $control.Current.IsEnabled) {
            return $control
        }
        Start-Sleep -Milliseconds 300
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "O controle '$id' não ficou disponível."
}

function Invoke-Control($control) {
    $control.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke()
}

function Wait-Operation($control, [int]$timeoutSeconds = 60) {
    $deadline = [DateTime]::UtcNow.AddSeconds($timeoutSeconds)
    $becameBusy = $false
    do {
        if (-not $control.Current.IsEnabled) { $becameBusy = $true }
        if ($becameBusy -and $control.Current.IsEnabled) { return }
        Start-Sleep -Milliseconds 200
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'A operação do instalador não terminou no tempo esperado.'
}

$result = [System.Collections.Generic.List[string]]::new()
$launcher = Start-Process -FilePath $Installer -PassThru
$process = $launcher
try {
    $installer = Find-InstallerWindow $launcher
    $root = $installer.Window
    $process = $installer.Process
    $uninstall = Wait-Control $root 'UninstallButton'
    Invoke-Control $uninstall
    $install = Wait-Control $root 'InstallButton'
    if (Test-Path 'C:\Users\Public\Desktop\Compasso.lnk') {
        throw 'A desinstalação não removeu o atalho da área de trabalho.'
    }
    $result.Add('UNINSTALL=PASS')

    $desktopOption = Wait-Control $root 'DesktopShortcutCheckBox'
    $toggle = $desktopOption.GetCurrentPattern([System.Windows.Automation.TogglePattern]::Pattern)
    if ($toggle.Current.ToggleState -ne [System.Windows.Automation.ToggleState]::On) {
        $toggle.Toggle()
    }
    Invoke-Control $install
    [void](Wait-Control $root 'UninstallButton')
    if (-not (Test-Path 'C:\Users\Public\Desktop\Compasso.lnk')) {
        throw 'A instalação não criou o atalho da área de trabalho.'
    }
    $result.Add('DESKTOP_SHORTCUT=PASS')
    $result.Add('REINSTALL_SAME_SESSION=PASS')

    $toggle.Toggle()
    Invoke-Control $install
    Wait-Operation $install
    if (Test-Path 'C:\Users\Public\Desktop\Compasso.lnk') {
        throw 'A atualização não removeu o atalho após desmarcar a opção.'
    }
    $result.Add('DESKTOP_SHORTCUT_DISABLE=PASS')

    $toggle.Toggle()
    Invoke-Control $install
    Wait-Operation $install
    if (-not (Test-Path 'C:\Users\Public\Desktop\Compasso.lnk')) {
        throw 'A atualização não recriou o atalho após marcar a opção.'
    }
    $result.Add('DESKTOP_SHORTCUT_REENABLE=PASS')

    $close = Wait-Control $root 'CancelButton'
    $timer = [Diagnostics.Stopwatch]::StartNew()
    Invoke-Control $close
    if (-not $process.WaitForExit(5000)) { throw 'O instalador não fechou em cinco segundos.' }
    $timer.Stop()
    $result.Add("CLOSE_MS=$($timer.ElapsedMilliseconds)")
    $result.Add('CLOSE=PASS')
    $result | Set-Content -Encoding utf8 $ResultPath
}
catch {
    $result.Add("FAIL=$($_.Exception.Message)")
    $result.Add("STACK=$($_.ScriptStackTrace)")
    $result | Set-Content -Encoding utf8 $ResultPath
    if (-not $process.HasExited) { $process.Kill($true) }
    throw
}
finally {
    $process.Dispose()
    if ($launcher.Id -ne $process.Id) { $launcher.Dispose() }
}
