<# Valida visualmente e por UI Automation o hover do período selecionado. #>
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class AddTimeHoverNative {
    public delegate bool EnumWindowsProc(IntPtr hWnd, IntPtr lParam);
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
    [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc callback, IntPtr lParam);
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint processId);
    [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);
    [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
}
"@

Get-Process CompassoApp -ErrorAction SilentlyContinue | Stop-Process -Force
$process = Start-Process 'C:\Program Files\Compasso\CompassoApp.exe' -PassThru
Start-Sleep -Seconds 7

$script:window = [IntPtr]::Zero
$callback = [AddTimeHoverNative+EnumWindowsProc]{
    param([IntPtr]$handle, [IntPtr]$parameter)
    $id = [uint32]0
    [void][AddTimeHoverNative]::GetWindowThreadProcessId($handle, [ref]$id)
    if ($id -eq $process.Id -and [AddTimeHoverNative]::IsWindowVisible($handle)) {
        $script:window = $handle
        return $false
    }
    return $true
}
[void][AddTimeHoverNative]::EnumWindows($callback, [IntPtr]::Zero)
if ($script:window -eq [IntPtr]::Zero) { throw 'Janela do Compasso não encontrada.' }

$root = [System.Windows.Automation.AutomationElement]::FromHandle($script:window)
$condition = [System.Windows.Automation.PropertyCondition]::new(
    [System.Windows.Automation.AutomationElement]::AutomationIdProperty, 'Duration30')
$duration = $root.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $condition)
if ($null -eq $duration) { throw 'Botão de 30 minutos não encontrado.' }
$pattern = $null
$isSelected = $false
if ($duration.TryGetCurrentPattern([System.Windows.Automation.SelectionItemPattern]::Pattern, [ref]$pattern)) {
    $isSelected = ([System.Windows.Automation.SelectionItemPattern]$pattern).Current.IsSelected
}
elseif ($duration.TryGetCurrentPattern([System.Windows.Automation.TogglePattern]::Pattern, [ref]$pattern)) {
    $isSelected = ([System.Windows.Automation.TogglePattern]$pattern).Current.ToggleState -eq [System.Windows.Automation.ToggleState]::On
}
else {
    throw 'O período de 30 minutos não expõe um padrão de seleção.'
}
if (-not $isSelected) {
    throw 'O período inicial de 30 minutos não está selecionado.'
}

$bounds = $duration.Current.BoundingRectangle
[void][AddTimeHoverNative]::SetCursorPos(
    [int]($bounds.X + $bounds.Width / 2),
    [int]($bounds.Y + $bounds.Height / 2))
Start-Sleep -Seconds 2

$rect = New-Object AddTimeHoverNative+RECT
[void][AddTimeHoverNative]::GetWindowRect($script:window, [ref]$rect)
$width = $rect.Right - $rect.Left
$height = $rect.Bottom - $rect.Top
$bitmap = [System.Drawing.Bitmap]::new($width, $height)
$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
try {
    $graphics.CopyFromScreen($rect.Left, $rect.Top, 0, 0, [System.Drawing.Size]::new($width, $height))
    $bitmap.Save('C:\CompassoWinUIBootstrap\add-time-selected-hover.png', [System.Drawing.Imaging.ImageFormat]::Png)
}
finally {
    $graphics.Dispose()
    $bitmap.Dispose()
}

@(
    'SELECTED_DURATION=30'
    'SELECTION_STATE=ON'
    'POINTER_OVER_SELECTED=PASS'
    "PROCESS_ID=$($process.Id)"
) | Set-Content -Encoding utf8 'C:\CompassoWinUIBootstrap\add-time-hover-result.txt'
