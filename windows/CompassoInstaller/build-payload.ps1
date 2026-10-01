<#
.SYNOPSIS
    Monta o pacote do instalador: copia o aplicativo do Compasso para a pasta de
    payload e escreve o payload.json que descreve a instalação.

.DESCRIPTION
    O instalador não conhece o aplicativo. Ele transporta arquivos declarados no
    payload.json e aplica o que estiver lá. Este script é a etapa que liga os
    dois: escolhe a saída compilada do aplicativo, copia a árvore inteira
    preservando subpastas e descreve os arquivos, o atalho do menu Iniciar e a
    entrada de desinstalação.

    Nada de serviço, por decisão explícita: esta etapa instala apenas a interface.
#>
[CmdletBinding()]
param(
    [string]$AppPublishDirectory,
    [string]$OutputDirectory,
    [string]$Configuration = 'Debug',
    [string]$Platform = 'x64',
    [string]$TargetFramework = 'net10.0-windows10.0.26100.0',
    [string]$RuntimeIdentifier = 'win-x64',
    [string]$Version = '0.1.0'
)

$ErrorActionPreference = 'Stop'

# O instalador procura o payload ao lado do próprio executável, em
# AppContext.BaseDirectory. Gerar na pasta do projeto não funcionaria: o
# manifesto precisa cair na mesma pasta do exe que vai lê-lo.
$binDirectory = Join-Path $PSScriptRoot "bin\$Platform\$Configuration\$TargetFramework\$RuntimeIdentifier"
$appDirectory = Join-Path $PSScriptRoot "..\CompassoApp\bin\$Platform\$Configuration\$TargetFramework\$RuntimeIdentifier"

if (-not $AppPublishDirectory) { $AppPublishDirectory = $appDirectory }
if (-not $OutputDirectory)     { $OutputDirectory = $binDirectory }

if (-not (Test-Path (Join-Path $AppPublishDirectory 'CompassoApp.exe'))) {
    throw "Não encontrei CompassoApp.exe em '$AppPublishDirectory'. Compile o aplicativo antes."
}

# O instalador procura o manifesto em AppContext.BaseDirectory, e as origens do
# plano são resolvidas a partir da mesma pasta. Por isso o payload.json fica
# solto ao lado do exe, e a árvore do aplicativo numa subpasta: o manifesto
# precisa estar onde o processo procura, não onde seria mais organizado.
$payloadDir = Join-Path $OutputDirectory 'payload'
$appDir = Join-Path $payloadDir 'app'

Write-Host "Origem:  $AppPublishDirectory"
Write-Host "Payload: $payloadDir"

# Reconstrói a pasta de payload para nunca embaralhar arquivos de uma build
# anterior com a de agora.
if (Test-Path $payloadDir) { Remove-Item $payloadDir -Recurse -Force }
New-Item -ItemType Directory -Path $appDir -Force | Out-Null

Copy-Item (Join-Path $AppPublishDirectory '*') $appDir -Recurse -Force

# O caminho versionado pelo conteúdo evita que o Shell reutilize uma entrada
# antiga do cache quando o ícone mudar entre builds.
$sourceIcon = Join-Path $appDir 'Assets\AppIcon.ico'
$iconHash = (Get-FileHash $sourceIcon -Algorithm SHA256).Hash.Substring(0, 12).ToLowerInvariant()
$versionedIconName = "AppIcon-$iconHash.ico"
Copy-Item $sourceIcon (Join-Path $appDir "Assets\$versionedIconName") -Force

# O executável do aplicativo precisa ficar com nome próprio dentro do plano: é
# ele que o atalho aponta e que a desinstalação usa para avisar que está aberto.
$files = @()
foreach ($source in Get-ChildItem $appDir -Recurse -File) {
    $relative = $source.FullName.Substring($appDir.Length + 1)
    $files += [ordered]@{
        source      = ('payload/app/' + ($relative -replace '\\', '/'))
        destination = ($relative -replace '\\', '/')
    }
}

$installDirectory = 'C:\Program Files\Compasso'
$startMenuDirectory = 'C:\ProgramData\Microsoft\Windows\Start Menu\Programs\Compasso'
$appExe = Join-Path $installDirectory 'CompassoApp.exe'
$appIcon = Join-Path $installDirectory "Assets\$versionedIconName"

# A desinstalação reabre o próprio instalador, que detecta o produto instalado e
# oferece a remoção. O caminho precisa ser o do executável compilado, que fica em
# $OutputDirectory ao lado do payload.json — usar $PSScriptRoot gravava um caminho
# para a pasta do projeto, onde não existe executável nenhum.
$installerExe = Join-Path $OutputDirectory 'CompassoInstaller.exe'
$uninstallString = '"{0}"' -f $installerExe

$plan = [ordered]@{
    productName      = 'Compasso'
    version          = $Version
    installDirectory = $installDirectory
    startMenuDirectory = $startMenuDirectory
    files            = $files
    shortcuts        = @(
        [ordered]@{
            target       = $appExe
            shortcutPath = (Join-Path $startMenuDirectory 'Compasso.lnk')
            description  = 'Compasso - adicionar tempo e configurar o computador'
            # IShellLink recebe o índice separadamente. Incluir ",0" aqui
            # produziria "arquivo.ico,0,0" e o Windows exibiria ícone vazio.
            iconLocation = $appIcon
        }
    )
    uninstall        = [ordered]@{
        keyPath         = 'SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Compasso'
        displayName     = 'Compasso'
        displayVersion  = $Version
        publisher       = 'Compasso'
        uninstallString = $uninstallString
        noModify        = 1
        noRepair        = 1
    }
}

$plan | ConvertTo-Json -Depth 6 | Set-Content -Encoding utf8 (Join-Path $OutputDirectory 'payload.json')


Write-Host ("Arquivos no payload: {0}" -f $files.Count)
Write-Host ("Atalho:             {0}" -f (Join-Path $startMenuDirectory 'Compasso.lnk'))
Write-Host ("Desinstalar via:    {0}" -f $uninstallString)
Write-Host 'Payload pronto.'
