$ErrorActionPreference = 'Stop'
$installDir = 'C:\Program Files\Compasso'
$startShortcut = 'C:\ProgramData\Microsoft\Windows\Start Menu\Programs\Compasso.lnk'
$desktopShortcut = 'C:\Users\Public\Desktop\Compasso.lnk'
$setup = 'C:\CompassoWinUIBootstrap\unpackaged\artifacts\CompassoSetup.exe'
$result = 'C:\CompassoWinUIBootstrap\wails-install-cycle.txt'

if (-not (Test-Path $setup)) { throw 'CompassoSetup.exe não encontrado.' }

function Install-Compasso {
    $process = Start-Process $setup -ArgumentList '/VERYSILENT','/SUPPRESSMSGBOXES','/NORESTART','/TASKS=desktopicon' -Wait -PassThru
    if ($process.ExitCode -ne 0) { throw "A instalação falhou com código $($process.ExitCode)." }
}

Get-Process Compasso -ErrorAction SilentlyContinue | Stop-Process -Force
$uninstaller = Join-Path $installDir 'unins000.exe'

# O ciclo deve ser reproduzível tanto numa máquina limpa quanto depois de uma
# instalação anterior. Primeiro garante uma instalação válida para então
# exercitar a desinstalação.
if (-not (Test-Path $uninstaller)) { Install-Compasso }
if (-not (Test-Path $uninstaller)) { throw 'A instalação inicial não criou o desinstalador.' }

$process = Start-Process $uninstaller -ArgumentList '/VERYSILENT','/SUPPRESSMSGBOXES','/NORESTART' -Wait -PassThru
if ($process.ExitCode -ne 0) { throw "A desinstalação falhou com código $($process.ExitCode)." }
Start-Sleep -Seconds 2
$lines = @(
    "INSTALL_DIR_AFTER_UNINSTALL=$(Test-Path $installDir)"
    "START_SHORTCUT_AFTER_UNINSTALL=$(Test-Path $startShortcut)"
    "DESKTOP_SHORTCUT_AFTER_UNINSTALL=$(Test-Path $desktopShortcut)"
)
if (Test-Path $installDir) { throw 'A desinstalação não removeu a pasta do aplicativo.' }
if (Test-Path $startShortcut) { throw 'A desinstalação não removeu o atalho do menu Iniciar.' }
if (Test-Path $desktopShortcut) { throw 'A desinstalação não removeu o atalho da área de trabalho.' }
if (Get-Service CompassoAgent -ErrorAction SilentlyContinue) { throw 'A desinstalação não removeu o serviço CompassoAgent.' }
$lines += 'SERVICE_AFTER_UNINSTALL=REMOVED'

Install-Compasso
$installedExe = Join-Path $installDir 'Compasso.exe'
$installedAgent = Join-Path $installDir 'CompassoAgent.exe'
$lines += @(
    "INSTALL_DIR_AFTER_REINSTALL=$(Test-Path $installedExe)"
    "START_SHORTCUT_AFTER_REINSTALL=$(Test-Path $startShortcut)"
    "DESKTOP_SHORTCUT_AFTER_REINSTALL=$(Test-Path $desktopShortcut)"
)
if (-not (Test-Path $installedExe)) { throw 'A reinstalação não criou Compasso.exe.' }
if (-not (Test-Path $installedAgent)) { throw 'A reinstalação não criou CompassoAgent.exe.' }
if (-not (Test-Path $startShortcut)) { throw 'A reinstalação não criou o atalho do menu Iniciar.' }
if (-not (Test-Path $desktopShortcut)) { throw 'A reinstalação não criou o atalho da área de trabalho.' }
$service = Get-Service CompassoAgent -ErrorAction Stop
$serviceConfiguration = Get-CimInstance Win32_Service -Filter "Name='CompassoAgent'"
if ($serviceConfiguration.StartMode -ne 'Auto') { throw "O serviço não ficou automático: $($serviceConfiguration.StartMode)" }
if (Test-Path 'C:\ProgramData\Compasso\agent-config.json') {
    $service.WaitForStatus('Running', [TimeSpan]::FromSeconds(20))
}
$runtimeFiles = @(Get-ChildItem $installDir -File -Recurse | Where-Object {
    $_.Extension -eq '.dll' -or $_.Name -in @('dotnet.exe', 'hostfxr.dll', 'hostpolicy.dll')
})
if ($runtimeFiles.Count -ne 0) {
    throw "A instalação contém arquivos de runtime inesperados: $($runtimeFiles.Name -join ', ')"
}
$lines += "SETUP_BYTES=$((Get-Item $setup).Length)"
$lines += "APP_BYTES=$((Get-Item $installedExe).Length)"
$lines += "AGENT_BYTES=$((Get-Item $installedAgent).Length)"
$lines += "SERVICE_STATUS=$($service.Status)"
$lines += "SERVICE_START_MODE=$($serviceConfiguration.StartMode)"
$lines += "INSTALLED_FILES=$((Get-ChildItem $installDir -File -Recurse).Count)"
$lines += 'DOTNET_RUNTIME_FILES=0'
$lines += 'INSTALL_CYCLE=PASS'
$lines | Set-Content -Encoding utf8 $result
