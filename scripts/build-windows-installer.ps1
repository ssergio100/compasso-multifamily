param(
    [string]$InnoCompiler = '',
    [string]$Version = ''
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$binaryDirectory = Join-Path $projectRoot 'dist\windows'
$outputDirectory = Join-Path $projectRoot 'dist'
$installerSource = Join-Path $projectRoot 'packaging\windows\compasso-agent.iss'
$licensePath = Join-Path $projectRoot 'LICENSE'

if ([string]::IsNullOrWhiteSpace($Version)) {
    $controlPath = Join-Path $projectRoot 'packaging\debian\control'
    $versionLine = Select-String -Path $controlPath -Pattern '^Version:\s*(.+)$'
    if ($null -eq $versionLine) {
        throw 'Could not read the package version.'
    }
    $Version = $versionLine.Matches[0].Groups[1].Value.Replace('~', '-')
}

$numericVersion = '0.0.0.0'
if ($Version -match '^(\d+)\.(\d+)\.(\d+)-pilot(\d+)$') {
    $numericVersion = '{0}.{1}.{2}.{3}' -f `
        $Matches[1], $Matches[2], $Matches[3], $Matches[4]
} elseif ($Version -match '^(\d+)\.(\d+)\.(\d+)$') {
    $numericVersion = '{0}.{1}.{2}.0' -f `
        $Matches[1], $Matches[2], $Matches[3]
} else {
    throw "Version '$Version' cannot be converted to Windows version metadata."
}

foreach ($binaryName in @(
    'compasso-windows-service.exe',
    'compasso-windows-companion.exe'
)) {
    $binaryPath = Join-Path $binaryDirectory $binaryName
    if (-not (Test-Path -LiteralPath $binaryPath -PathType Leaf)) {
        throw "Missing Windows binary: $binaryPath"
    }
}

if ([string]::IsNullOrWhiteSpace($InnoCompiler)) {
    $compilerCandidates = @(
        (Join-Path $env:LOCALAPPDATA 'Programs\Inno Setup 6\ISCC.exe'),
        (Join-Path ${env:ProgramFiles(x86)} 'Inno Setup 6\ISCC.exe'),
        (Join-Path $env:ProgramFiles 'Inno Setup 6\ISCC.exe')
    )
    $InnoCompiler = $compilerCandidates |
        Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } |
        Select-Object -First 1
}

if ([string]::IsNullOrWhiteSpace($InnoCompiler) -or
    -not (Test-Path -LiteralPath $InnoCompiler -PathType Leaf)) {
    throw 'Inno Setup 6 compiler was not found. Pass its path with -InnoCompiler.'
}

New-Item -ItemType Directory -Path $outputDirectory -Force | Out-Null
& $InnoCompiler `
    "/DAppVersion=$Version" `
    "/DNumericVersion=$numericVersion" `
    "/DBinaryDir=$binaryDirectory" `
    "/DOutputDir=$outputDirectory" `
    "/DLicensePath=$licensePath" `
    $installerSource
if ($LASTEXITCODE -ne 0) {
    throw "Inno Setup failed with exit code $LASTEXITCODE."
}

$installerPath = Join-Path $outputDirectory `
    "CompassoAgent-$Version-windows-x64.exe"
if (-not (Test-Path -LiteralPath $installerPath -PathType Leaf)) {
    throw "Installer was not generated: $installerPath"
}

Write-Output "Windows installer created at $installerPath"
