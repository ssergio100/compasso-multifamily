<# Compila o serviço Windows do Compasso com o SQLite incorporado via CGO. #>
[CmdletBinding()]
param(
    [string]$OutputPath
)

$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($OutputPath)) {
    $OutputPath = Join-Path $PSScriptRoot 'build\agent\CompassoAgent.exe'
}
$repositoryRoot = Split-Path $PSScriptRoot -Parent
$outputDirectory = Split-Path $OutputPath -Parent
New-Item -ItemType Directory -Path $outputDirectory -Force | Out-Null

$previousCgo = $env:CGO_ENABLED
$previousCc = $env:CC
$previousPath = $env:Path
$gcc = 'C:\msys64\ucrt64\bin\gcc.exe'
if (-not (Test-Path -LiteralPath $gcc -PathType Leaf)) {
    throw "GCC do MSYS2 não encontrado em $gcc. Instale o ambiente UCRT64 do MSYS2."
}
$env:CGO_ENABLED = '1'
$env:CC = $gcc
$env:Path = "$(Split-Path $gcc -Parent);$env:Path"
Push-Location $repositoryRoot
try {
    go build -trimpath -o $OutputPath ./agent/cmd/compasso-agent-windows
    if ($LASTEXITCODE -ne 0) {
        throw "A build do serviço falhou com código $LASTEXITCODE."
    }
}
finally {
    Pop-Location
    $env:CGO_ENABLED = $previousCgo
    $env:CC = $previousCc
    $env:Path = $previousPath
}

$binary = Get-Item $OutputPath
$hash = (Get-FileHash $binary.FullName -Algorithm SHA256).Hash
Write-Host "Serviço: $($binary.FullName) ($($binary.Length) bytes)"
Write-Host "SHA-256: $hash"
