<# Valida o ciclo de vida mínimo do serviço e restaura o SCM ao final. #>
[CmdletBinding()]
param(
    [string]$AgentPath
)

$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($AgentPath)) {
    $AgentPath = Join-Path $PSScriptRoot 'build\agent\CompassoAgent.exe'
}
$serviceName = 'CompassoAgent'
$statePath = Join-Path $env:ProgramData 'Compasso\service-state.json'
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Execute este verificador em um PowerShell elevado.'
}
if (-not (Test-Path $AgentPath)) {
    throw "Executável do serviço ausente: $AgentPath"
}
if (Get-Service -Name $serviceName -ErrorAction SilentlyContinue) {
    throw "O serviço $serviceName já existe; o teste não alterará uma instalação anterior."
}

$installedByTest = $false
try {
    & $AgentPath install
    if ($LASTEXITCODE -ne 0) { throw "INSTALL=FAIL ($LASTEXITCODE)" }
    $installedByTest = $true
    Write-Host 'INSTALL=PASS'

    & $AgentPath start
    if ($LASTEXITCODE -ne 0) { throw "START=FAIL ($LASTEXITCODE)" }
    $service = Get-Service -Name $serviceName
    if ($service.Status -ne 'Running') { throw "START=FAIL ($($service.Status))" }
    $state = Get-Content $statePath -Raw | ConvertFrom-Json
    if ($state.schema_version -ne 1 -or $state.state -ne 'running') {
        throw 'RUNNING_STATE=FAIL'
    }
    Write-Host 'START=PASS'
    Write-Host 'RUNNING_STATE=PASS'

    & $AgentPath stop
    if ($LASTEXITCODE -ne 0) { throw "STOP=FAIL ($LASTEXITCODE)" }
    $state = Get-Content $statePath -Raw | ConvertFrom-Json
    if ($state.state -ne 'stopped') { throw 'STOPPED_STATE=FAIL' }
    Write-Host 'STOP=PASS'
    Write-Host 'STOPPED_STATE=PASS'

    & $AgentPath start
    if ($LASTEXITCODE -ne 0) { throw "RESTART=FAIL ($LASTEXITCODE)" }
    $state = Get-Content $statePath -Raw | ConvertFrom-Json
    if ($state.state -ne 'running') { throw 'RESTART_STATE=FAIL' }
    Write-Host 'RESTART=PASS'
}
finally {
    if ($installedByTest) {
        & $AgentPath stop 2>$null
        & $AgentPath uninstall
        if ($LASTEXITCODE -eq 0) {
            Write-Host 'UNINSTALL=PASS'
        }
        else {
            Write-Warning "UNINSTALL=FAIL ($LASTEXITCODE)"
        }
    }
}
