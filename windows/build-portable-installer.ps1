<# Compila o Wails v2 e gera o único artefato distribuível com Inno Setup. #>
[CmdletBinding()]
param([string]$Version = '0.1.0')

$ErrorActionPreference = 'Stop'
$wailsProject = Join-Path $PSScriptRoot 'CompassoWails'
$wailsExe = Join-Path $env:USERPROFILE 'go\bin\wails.exe'
$goExe = Join-Path $env:ProgramFiles 'Go\bin\go.exe'
$nodeDirectory = Join-Path $env:ProgramFiles 'nodejs'
$wailsIcon = Join-Path $wailsProject 'build\appicon.png'
$compiledApp = Join-Path $wailsProject 'build\bin\Compasso.exe'
$compiledAgent = Join-Path $PSScriptRoot 'build\agent\CompassoAgent.exe'
$artifactDirectory = Join-Path $PSScriptRoot 'artifacts'
$artifact = Join-Path $artifactDirectory 'CompassoSetup.exe'
$setupScript = Join-Path $PSScriptRoot 'CompassoSetup.iss'
$isccCandidates = @(
    (Join-Path $env:LOCALAPPDATA 'Programs\Inno Setup 6\ISCC.exe'),
    (Join-Path $env:ProgramFiles 'Inno Setup 6\ISCC.exe'),
    (Join-Path ${env:ProgramFiles(x86)} 'Inno Setup 6\ISCC.exe')
)
$iscc = $isccCandidates | Where-Object { $_ -and (Test-Path $_) } | Select-Object -First 1

foreach ($required in @($wailsExe, $goExe, $wailsIcon, $setupScript)) {
    if (-not (Test-Path $required)) { throw "Ferramenta ou fonte obrigatória ausente: $required" }
}
if (-not $iscc) { throw 'Inno Setup 6 não encontrado.' }

& (Join-Path $PSScriptRoot 'build-agent-service.ps1') -OutputPath $compiledAgent
if ($LASTEXITCODE -ne 0) { throw "A build do serviço falhou com código $LASTEXITCODE." }

$env:Path = "$(Split-Path $goExe);$nodeDirectory;$(Split-Path $wailsExe);$env:Path"

Push-Location $wailsProject
try {
    & $wailsExe build -clean -platform windows/amd64 -o Compasso.exe -webview2 error
    if ($LASTEXITCODE -ne 0) { throw "A build Wails falhou com código $LASTEXITCODE." }
}
finally {
    Pop-Location
}

if (-not (Test-Path $compiledApp)) { throw "Executável Wails ausente: $compiledApp" }
if (-not (Test-Path $compiledAgent)) { throw "Executável do serviço ausente: $compiledAgent" }
if (Test-Path $artifactDirectory) { Remove-Item $artifactDirectory -Recurse -Force }
New-Item -ItemType Directory -Path $artifactDirectory -Force | Out-Null

$generatedIcon = Join-Path $wailsProject 'build\windows\icon.ico'
& $iscc "/DSourceExe=$compiledApp" "/DSourceAgent=$compiledAgent" "/DSetupIcon=$generatedIcon" `
    "/DOutputDir=$artifactDirectory" "/DMyAppVersion=$Version" $setupScript
if ($LASTEXITCODE -ne 0) { throw "A geração do instalador falhou com código $LASTEXITCODE." }
if (-not (Test-Path $artifact)) { throw "Executável final ausente: $artifact" }

$rootFiles = @(Get-ChildItem $artifactDirectory -File)
if ($rootFiles.Count -ne 1 -or $rootFiles[0].Name -ne 'CompassoSetup.exe') {
    throw 'A pasta de entrega deve conter somente CompassoSetup.exe.'
}

$appSize = (Get-Item $compiledApp).Length
$agentSize = (Get-Item $compiledAgent).Length
$setupSize = (Get-Item $artifact).Length
$hash = (Get-FileHash $artifact -Algorithm SHA256).Hash
Write-Host "Executável Wails embutido: $compiledApp ($appSize bytes)"
Write-Host "Serviço embutido: $compiledAgent ($agentSize bytes)"
Write-Host "Artefato único: $artifact ($setupSize bytes)"
Write-Host "SHA-256: $hash"
