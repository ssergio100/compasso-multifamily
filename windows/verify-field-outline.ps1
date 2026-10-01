<# Captura os campos contornados com foco nas duas interfaces instaladas. #>
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class CompassoFieldOutlineNative {
    public delegate bool EnumWindowsProc(IntPtr hWnd, IntPtr lParam);
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
    [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc callback, IntPtr lParam);
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint processId);
    [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
    [DllImport("dwmapi.dll")] public static extern int DwmFlush();
}
"@

$base = 'C:\CompassoWinUIBootstrap'
$resultPath = Join-Path $base 'field-outline-result.txt'

function Find-ById([System.Windows.Automation.AutomationElement]$root, [string]$id) {
    $condition = [System.Windows.Automation.PropertyCondition]::new(
        [System.Windows.Automation.AutomationElement]::AutomationIdProperty,
        $id)
    return $root.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $condition)
}

function Capture-Window([string]$path) {
    $rect = New-Object CompassoFieldOutlineNative+RECT
    [void][CompassoFieldOutlineNative]::GetWindowRect($script:window, [ref]$rect)
    $width = $rect.Right - $rect.Left
    $height = $rect.Bottom - $rect.Top
    [void][CompassoFieldOutlineNative]::SetForegroundWindow($script:window)
    [void][CompassoFieldOutlineNative]::DwmFlush()
    Start-Sleep -Milliseconds 500
    $bitmap = [System.Drawing.Bitmap]::new($width, $height)
    $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
    try {
        $graphics.CopyFromScreen($rect.Left, $rect.Top, 0, 0, [System.Drawing.Size]::new($width, $height))
        $bitmap.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
    }
    finally {
        $graphics.Dispose()
        $bitmap.Dispose()
    }
}

try {
    Get-Process CompassoApp -ErrorAction SilentlyContinue | Stop-Process -Force
    $process = Start-Process 'C:\Program Files\Compasso\CompassoApp.exe' -PassThru
    Start-Sleep -Seconds 7

    $script:window = [IntPtr]::Zero
    $callback = [CompassoFieldOutlineNative+EnumWindowsProc]{
        param([IntPtr]$handle, [IntPtr]$parameter)
        $id = [uint32]0
        [void][CompassoFieldOutlineNative]::GetWindowThreadProcessId($handle, [ref]$id)
        if ($id -eq $process.Id -and [CompassoFieldOutlineNative]::IsWindowVisible($handle)) {
            $script:window = $handle
            return $false
        }
        return $true
    }
    [void][CompassoFieldOutlineNative]::EnumWindows($callback, [IntPtr]::Zero)
    if ($script:window -eq [IntPtr]::Zero) { throw 'Janela do Compasso não encontrada.' }

    $root = [System.Windows.Automation.AutomationElement]::FromHandle($script:window)
    $password = Find-ById $root 'PasswordBox'
    if ($null -eq $password) { throw 'Campo de senha não encontrado.' }
    $password.SetFocus()
    Capture-Window (Join-Path $base 'field-outline-add-time-focused.png')

    $settings = Find-ById $root 'OpenSettingsButton'
    if ($null -eq $settings) { throw 'Acesso às configurações não encontrado.' }
    $settings.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke()
    Start-Sleep -Seconds 2
    $root = [System.Windows.Automation.AutomationElement]::FromHandle($script:window)

    $server = Find-ById $root 'ServerBox'
    if ($null -eq $server) { throw 'Campo de servidor não encontrado.' }
    $server.SetFocus()
    Capture-Window (Join-Path $base 'field-outline-settings-text-focused.png')

    $token = Find-ById $root 'TokenBox'
    if ($null -eq $token) { throw 'Campo de token não encontrado.' }
    $token.SetFocus()
    Capture-Window (Join-Path $base 'field-outline-settings-password-focused.png')

    @(
        'FIELD_OUTLINE_FOCUS_CAPTURE=PASS'
        'ADD_TIME_PASSWORD=PASS'
        'SETTINGS_TEXT=PASS'
        'SETTINGS_PASSWORD=PASS'
        "PROCESS_ID=$($process.Id)"
    ) | Set-Content -Encoding utf8 $resultPath
}
catch {
    @('FIELD_OUTLINE_FOCUS_CAPTURE=FAIL', "ERROR=$($_.Exception.Message)") |
        Set-Content -Encoding utf8 $resultPath
    throw
}
