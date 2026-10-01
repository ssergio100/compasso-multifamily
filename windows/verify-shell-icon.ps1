<# Captura reproduzível da busca do Windows para validar nome e ícone. #>
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.Windows.Forms

[System.Windows.Forms.SendKeys]::SendWait('^{ESC}')
Start-Sleep -Seconds 1
[System.Windows.Forms.SendKeys]::SendWait('Compasso')
Start-Sleep -Seconds 3

$bounds = [System.Windows.Forms.SystemInformation]::VirtualScreen
$bitmap = [System.Drawing.Bitmap]::new($bounds.Width, $bounds.Height)
$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
try {
    $graphics.CopyFromScreen($bounds.Left, $bounds.Top, 0, 0, $bounds.Size)
    $bitmap.Save('C:\CompassoWinUIBootstrap\shell-search-compasso.png', [System.Drawing.Imaging.ImageFormat]::Png)
}
finally {
    $graphics.Dispose()
    $bitmap.Dispose()
    [System.Windows.Forms.SendKeys]::SendWait('{ESC}')
}
