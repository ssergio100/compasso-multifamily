param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('Install', 'Stop', 'Uninstall')]
    [string]$Action,

    [string]$ExecutablePath,
    [string]$StateDirectory = "$env:ProgramData\Compasso\Agent"
)

$ErrorActionPreference = 'Stop'
$serviceName = 'CompassoAgent'

function Stop-AgentService {
    $service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
    if ($null -ne $service -and $service.Status -ne 'Stopped') {
        Stop-Service -Name $serviceName -Force
        $service.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(20))
    }
}

if ($Action -eq 'Stop') {
    Stop-AgentService
    exit 0
}

if ($Action -eq 'Uninstall') {
    Stop-AgentService
    if ($null -ne (Get-Service -Name $serviceName -ErrorAction SilentlyContinue)) {
        & "$env:SystemRoot\System32\sc.exe" delete $serviceName | Out-Null
        if ($LASTEXITCODE -ne 0) {
            throw "sc.exe delete failed with exit code $LASTEXITCODE"
        }
    }
    exit 0
}

if ([string]::IsNullOrWhiteSpace($ExecutablePath) -or
    -not [IO.Path]::IsPathRooted($ExecutablePath) -or
    -not (Test-Path -LiteralPath $ExecutablePath -PathType Leaf)) {
    throw 'The Windows service executable path is invalid.'
}

if (-not (Test-Path -LiteralPath $StateDirectory -PathType Container)) {
    New-Item -ItemType Directory -Path $StateDirectory -Force | Out-Null
}

# Configuration and the SQLite database contain enrollment credentials and are
# readable only by LocalSystem and the local Administrators group.
& "$env:SystemRoot\System32\icacls.exe" $StateDirectory /inheritance:r `
    /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw "icacls.exe failed with exit code $LASTEXITCODE"
}

Stop-AgentService
$binaryPath = ('"{0}" -service-name {1}' -f $ExecutablePath, $serviceName)
$service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
if ($null -eq $service) {
    New-Service -Name $serviceName -BinaryPathName $binaryPath `
        -DisplayName 'Compasso Agent' -StartupType Automatic | Out-Null
} else {
    $configuredPath = (Get-CimInstance -ClassName Win32_Service `
        -Filter "Name='$serviceName'").PathName
    if ($configuredPath -ne $binaryPath) {
        throw "The existing Compasso service points to an unexpected executable."
    }
    Set-Service -Name $serviceName -DisplayName 'Compasso Agent' `
        -StartupType Automatic
}

Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Services\$serviceName" `
    -Name Description -Type String `
    -Value 'Aplica localmente as políticas de tempo do Compasso.'

& "$env:SystemRoot\System32\cmd.exe" /d /s /c `
    'sc.exe failure CompassoAgent reset= 86400 actions= restart/5000/restart/15000/restart/60000' `
    | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw "sc.exe failure failed with exit code $LASTEXITCODE"
}
& "$env:SystemRoot\System32\cmd.exe" /d /s /c `
    'sc.exe failureflag CompassoAgent 1' | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw "sc.exe failureflag failed with exit code $LASTEXITCODE"
}

Start-Service -Name $serviceName
(Get-Service -Name $serviceName).WaitForStatus('Running', [TimeSpan]::FromSeconds(20))
