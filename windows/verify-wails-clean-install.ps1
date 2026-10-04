[CmdletBinding()]
param(
    [ValidateSet('clean-install', 'verify-clean-install', 'configured', 'post-reboot', 'update', 'uninstall')]
    [string]$Phase,
    [string]$SetupPath,
    [string]$StatePath = 'C:\CompassoWinUIBootstrap\compasso-clean-acceptance-state.json',
    [string]$ResultPath = 'C:\CompassoWinUIBootstrap\compasso-clean-acceptance.txt'
)

$ErrorActionPreference = 'Stop'
$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
if (-not $SetupPath) { $SetupPath = Join-Path $scriptRoot 'artifacts\CompassoSetup.exe' }
$installDir = 'C:\Program Files\Compasso'
$installedApp = Join-Path $installDir 'Compasso.exe'
$installedAgent = Join-Path $installDir 'CompassoAgent.exe'
$uninstaller = Join-Path $installDir 'unins000.exe'
$startShortcut = 'C:\ProgramData\Microsoft\Windows\Start Menu\Programs\Compasso.lnk'
$desktopShortcut = 'C:\Users\Public\Desktop\Compasso.lnk'
$programData = 'C:\ProgramData\Compasso'
$configuration = Join-Path $programData 'agent-config.json'
$setupMarker = Join-Path $programData 'setup-complete'
$sourceApp = Join-Path $scriptRoot 'CompassoWails\build\bin\Compasso.exe'
$sourceAgent = Join-Path $scriptRoot 'build\agent\CompassoAgent.exe'

function Assert-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw 'O aceite limpo exige PowerShell elevado.'
    }
}

function Invoke-CheckedProcess([string]$FilePath, [string[]]$Arguments) {
    $process = Start-Process $FilePath -ArgumentList $Arguments -Wait -PassThru
    if ($process.ExitCode -ne 0) {
        throw "'$FilePath' falhou com código $($process.ExitCode)."
    }
}

function Assert-Exists([string]$Path, [string]$Description) {
    if (-not (Test-Path $Path)) { throw "$Description ausente: $Path" }
}

function Assert-Absent([string]$Path, [string]$Description) {
    if (Test-Path $Path) { throw "$Description ainda existe: $Path" }
}

function Get-AgentService {
    return Get-CimInstance Win32_Service -Filter "Name='CompassoAgent'" -ErrorAction SilentlyContinue
}

function Assert-Service([string]$ExpectedState, [string]$ExpectedStartMode = 'Auto') {
    $service = Get-AgentService
    if ($null -eq $service) { throw 'O serviço CompassoAgent não existe.' }
    if ($service.StartMode -ne $ExpectedStartMode) {
        throw "Modo de início inesperado: $($service.StartMode); esperado: $ExpectedStartMode."
    }
    if ($service.State -ne $ExpectedState) {
        throw "Estado inesperado do serviço: $($service.State); esperado: $ExpectedState."
    }
    return $service
}

function Assert-InstalledPayload {
    Assert-Exists $installedApp 'Interface instalada'
    Assert-Exists $installedAgent 'Agente instalado'
    Assert-Exists $uninstaller 'Desinstalador'
    Assert-Exists $startShortcut 'Atalho do menu Iniciar'
    Assert-Exists $desktopShortcut 'Ícone da área de trabalho'

    $runtimeFiles = @(Get-ChildItem $installDir -File -Recurse | Where-Object {
        $_.Extension -eq '.dll' -or $_.Name -in @('dotnet.exe', 'hostfxr.dll', 'hostpolicy.dll')
    })
    if ($runtimeFiles.Count -ne 0) {
        throw "Runtime inesperado na instalação: $($runtimeFiles.Name -join ', ')."
    }

    foreach ($pair in @(@($sourceApp, $installedApp), @($sourceAgent, $installedAgent))) {
        Assert-Exists $pair[0] 'Binário de origem da build'
        $sourceHash = (Get-FileHash $pair[0] -Algorithm SHA256).Hash
        $installedHash = (Get-FileHash $pair[1] -Algorithm SHA256).Hash
        if ($sourceHash -ne $installedHash) {
            throw "O binário instalado não corresponde à build: $($pair[1])."
        }
    }
}

function Read-State {
    Assert-Exists $StatePath 'Estado do aceite'
    return Get-Content $StatePath -Raw | ConvertFrom-Json
}

function Save-State($State) {
    $directory = Split-Path $StatePath -Parent
    if ($directory) { New-Item -ItemType Directory -Path $directory -Force | Out-Null }
    $State | ConvertTo-Json | Set-Content -Encoding utf8 $StatePath
}

function Add-Result([string[]]$Lines) {
    $directory = Split-Path $ResultPath -Parent
    if ($directory) { New-Item -ItemType Directory -Path $directory -Force | Out-Null }
    $Lines | Add-Content -Encoding utf8 $ResultPath
}

function Get-BootTimestamp {
    return (Get-CimInstance Win32_OperatingSystem).LastBootUpTime.ToString('o')
}

Assert-Administrator

switch ($Phase) {
    'clean-install' {
        Assert-Exists $SetupPath 'CompassoSetup.exe'
        if (Test-Path $StatePath) {
            throw "Já existe um aceite em andamento: $StatePath"
        }

        Get-Process Compasso -ErrorAction SilentlyContinue | Stop-Process -Force
        if (Test-Path $uninstaller) {
            Invoke-CheckedProcess $uninstaller @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART')
        }
        Start-Sleep -Seconds 2
        Assert-Absent $installDir 'Diretório da instalação anterior'
        Assert-Absent $startShortcut 'Atalho anterior do menu Iniciar'
        Assert-Absent $desktopShortcut 'Ícone anterior da área de trabalho'
        if ($null -ne (Get-AgentService)) { throw 'O serviço anterior não foi removido.' }

        $backupPath = $null
        if (Test-Path $programData) {
            $backupPath = "$programData.acceptance-$((Get-Date).ToString('yyyyMMdd-HHmmss'))"
            if (Test-Path $backupPath) { throw "Backup de aceite já existe: $backupPath" }
            Move-Item $programData $backupPath
        }

        Assert-Absent $programData 'Estado em ProgramData antes da instalação limpa'
        Remove-Item $ResultPath -ErrorAction SilentlyContinue
        $state = [ordered]@{
            StartedAt = (Get-Date).ToString('o')
            BackupPath = $backupPath
            BootBeforeCleanInstall = Get-BootTimestamp
            ConfiguredAt = $null
            BootBeforeRestart = $null
            SetupHash = (Get-FileHash $SetupPath -Algorithm SHA256).Hash
        }
        Save-State $state

        Invoke-CheckedProcess $SetupPath @(
            '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/TASKS=desktopicon'
        )
        Assert-InstalledPayload
        [void](Assert-Service 'Stopped' 'Manual')
        Assert-Absent $configuration 'Configuração numa instalação limpa'
        Assert-Absent $setupMarker 'Marco de setup numa instalação limpa'

        Add-Result @(
            'CLEAN_PRECONDITION=PASS'
            'CLEAN_INSTALL=PASS'
            'SERVICE_WITHOUT_CONFIGURATION=MANUAL_STOPPED'
            "SETUP_SHA256=$($state.SetupHash)"
            "BACKUP_PATH=$backupPath"
        )
    }
    'verify-clean-install' {
        $state = Read-State
        Assert-InstalledPayload
        [void](Assert-Service 'Stopped' 'Manual')
        Assert-Absent $configuration 'Configuração numa instalação limpa'
        Assert-Absent $setupMarker 'Marco de setup numa instalação limpa'
        Add-Result @(
            'CLEAN_PRECONDITION=PASS'
            'CLEAN_INSTALL=PASS'
            'SERVICE_WITHOUT_CONFIGURATION=MANUAL_STOPPED'
            "SETUP_SHA256=$($state.SetupHash)"
            "BACKUP_PATH=$($state.BackupPath)"
        )
    }
    'configured' {
        $state = Read-State
        Assert-InstalledPayload
        Assert-Exists $configuration 'Configuração concluída'
        Assert-Exists $setupMarker 'Marco de setup concluído'
        [void](Assert-Service 'Running')
        $state.ConfiguredAt = (Get-Date).ToString('o')
        $state.BootBeforeRestart = Get-BootTimestamp
        Save-State $state
        Add-Result @('CONFIGURATION=PASS', 'SERVICE_AFTER_CONFIGURATION=RUNNING')
    }
    'post-reboot' {
        $state = Read-State
        $currentBoot = Get-BootTimestamp
        if ([DateTimeOffset]::Parse($currentBoot) -le [DateTimeOffset]::Parse($state.BootBeforeRestart)) {
            throw 'O Windows não reiniciou depois da configuração.'
        }
        Assert-InstalledPayload
        Assert-Exists $configuration 'Configuração depois do reinício'
        Assert-Exists $setupMarker 'Marco de setup depois do reinício'
        [void](Assert-Service 'Running')
        Add-Result @('WINDOWS_RESTART=PASS', 'SERVICE_AFTER_RESTART=RUNNING')
    }
    'update' {
        $state = Read-State
        Assert-Exists $configuration 'Configuração antes da atualização'
        Assert-Exists $setupMarker 'Marco de setup antes da atualização'
        Invoke-CheckedProcess $SetupPath @(
            '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/TASKS=desktopicon'
        )
        Assert-InstalledPayload
        Assert-Exists $configuration 'Configuração preservada na atualização'
        Assert-Exists $setupMarker 'Marco de setup preservado na atualização'
        [void](Assert-Service 'Running')
        Add-Result @('UPDATE=PASS', 'CONFIGURATION_AFTER_UPDATE=PRESERVED')
    }
    'uninstall' {
        $state = Read-State
        Assert-Exists $uninstaller 'Desinstalador'
        Get-Process Compasso -ErrorAction SilentlyContinue | Stop-Process -Force
        Invoke-CheckedProcess $uninstaller @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART')
        Start-Sleep -Seconds 2
        Assert-Absent $installDir 'Diretório instalado'
        Assert-Absent $startShortcut 'Atalho do menu Iniciar'
        Assert-Absent $desktopShortcut 'Ícone da área de trabalho'
        if ($null -ne (Get-AgentService)) { throw 'O serviço permaneceu após a desinstalação.' }
        $orphanExecutables = @(Get-ChildItem $programData -File -Recurse -ErrorAction SilentlyContinue |
            Where-Object { $_.Extension -eq '.exe' })
        if ($orphanExecutables.Count -ne 0) {
            throw "Executáveis órfãos em ProgramData: $($orphanExecutables.FullName -join ', ')."
        }
        Add-Result @('UNINSTALL=PASS', 'EXECUTABLE_ORPHANS=0', 'ACCEPTANCE=PASS')
    }
}
