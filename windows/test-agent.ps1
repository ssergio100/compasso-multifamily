<# Executa a suite do agente no Windows com o mesmo CGO usado pela build. #>
[CmdletBinding()]
param(
    [string[]]$Package = @('./agent/...')
)

$ErrorActionPreference = 'Stop'
$repositoryRoot = Split-Path $PSScriptRoot -Parent
$gcc = 'C:\msys64\ucrt64\bin\gcc.exe'
if (-not (Test-Path -LiteralPath $gcc -PathType Leaf)) {
    throw "GCC do MSYS2 não encontrado em $gcc. Instale o ambiente UCRT64 do MSYS2."
}

$previousCgo = $env:CGO_ENABLED
$previousCc = $env:CC
$previousPath = $env:Path
$env:CGO_ENABLED = '1'
$env:CC = $gcc
$env:Path = "$(Split-Path $gcc -Parent);$env:Path"
Push-Location $repositoryRoot
try {
    & go test @Package
    if ($LASTEXITCODE -ne 0) {
        throw "Os testes do agente falharam com código $LASTEXITCODE."
    }
}
finally {
    Pop-Location
    $env:CGO_ENABLED = $previousCgo
    $env:CC = $previousCc
    $env:Path = $previousPath
}