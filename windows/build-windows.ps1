<#
.SYNOPSIS
    Gera o aplicativo, o instalador e o payload como uma única unidade.

.DESCRIPTION
    O projeto do instalador referencia o aplicativo e sempre recompõe o
    payload depois da build. Este script é o ponto de entrada humano e também
    confere que o CompassoApp.exe empacotado é idêntico ao recém-compilado.
    Com -Install, aplica a atualização por cima da instalação existente; esse
    modo exige PowerShell elevado.
#>
[CmdletBinding()]
param(
    [ValidateSet('Debug', 'Release')]
    [string]$Configuration = 'Debug',
    [ValidateSet('x64', 'x86', 'ARM64')]
    [string]$Platform = 'x64',
    [switch]$Install
)

$ErrorActionPreference = 'Stop'
$targetFramework = 'net10.0-windows10.0.26100.0'
$runtimeIdentifier = "win-$($Platform.ToLowerInvariant())"
$installerProject = Join-Path $PSScriptRoot 'CompassoInstaller\CompassoInstaller.csproj'
$appOutput = Join-Path $PSScriptRoot "CompassoApp\bin\$Platform\$Configuration\$targetFramework\$runtimeIdentifier"
$installerOutput = Join-Path $PSScriptRoot "CompassoInstaller\bin\$Platform\$Configuration\$targetFramework\$runtimeIdentifier"

dotnet build $installerProject -c $Configuration -p:Platform=$Platform -p:RuntimeIdentifier=$runtimeIdentifier -v minimal
if ($LASTEXITCODE -ne 0) { throw "A build do pacote falhou com código $LASTEXITCODE." }

$builtApp = Join-Path $appOutput 'CompassoApp.exe'
$payloadApp = Join-Path $installerOutput 'payload\app\CompassoApp.exe'
$installer = Join-Path $installerOutput 'CompassoInstaller.exe'
foreach ($required in @($builtApp, $payloadApp, $installer, (Join-Path $installerOutput 'payload.json'))) {
    if (-not (Test-Path $required)) { throw "Artefato obrigatório ausente: $required" }
}

$verifiedFiles = 0
foreach ($source in Get-ChildItem $appOutput -Recurse -File) {
    $relative = $source.FullName.Substring($appOutput.Length + 1)
    $packaged = Join-Path (Join-Path $installerOutput 'payload\app') $relative
    if (-not (Test-Path $packaged)) {
        throw "Arquivo do aplicativo ausente no payload: $relative"
    }

    $sourceHash = (Get-FileHash $source.FullName -Algorithm SHA256).Hash
    $packagedHash = (Get-FileHash $packaged -Algorithm SHA256).Hash
    if ($sourceHash -ne $packagedHash) {
        throw "Arquivo divergente no payload: $relative"
    }
    $verifiedFiles++
}

if ($Install) {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw 'Use um PowerShell elevado para executar com -Install.'
    }

    $process = Start-Process -FilePath $installer -ArgumentList '--install-unattended' -Wait -PassThru
    if ($process.ExitCode -ne 0) { throw "A atualização falhou com código $($process.ExitCode)." }
}

Write-Host "Aplicativo: $builtApp"
Write-Host "Instalador: $installer"
Write-Host "Payload verificado: $verifiedFiles arquivos idênticos à build."
if ($Install) { Write-Host 'Instalação atualizada com sucesso.' }
