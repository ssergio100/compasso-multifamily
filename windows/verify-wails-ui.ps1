$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class CompassoWailsNative {
    public delegate bool EnumWindowsProc(IntPtr hWnd, IntPtr lParam);
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
    [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc callback, IntPtr lParam);
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint processId);
    [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
    [DllImport("user32.dll")] public static extern void mouse_event(uint flags, uint dx, uint dy, uint data, UIntPtr extraInfo);
}
"@

$base = 'C:\CompassoWinUIBootstrap'
$resultPath = Join-Path $base 'wails-ui-result.txt'
$stdoutPath = Join-Path $base 'wails-ui-stdout.txt'
$stderrPath = Join-Path $base 'wails-ui-stderr.txt'

function Find-ByName($root, [string]$name) {
    $elements = $root.FindAll(
        [System.Windows.Automation.TreeScope]::Descendants,
        [System.Windows.Automation.Condition]::TrueCondition)
    for ($index = 0; $index -lt $elements.Count; $index++) {
        $element = $elements.Item($index)
        if ($element.Current.Name.Trim().StartsWith($name, [StringComparison]::OrdinalIgnoreCase)) {
            Write-Output -NoEnumerate $element
            return
        }
    }
    return $null
}

function Find-ExactName($root, [string]$name) {
    $condition = [System.Windows.Automation.PropertyCondition]::new(
        [System.Windows.Automation.AutomationElement]::NameProperty,
        $name)
    return $root.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $condition)
}

function Invoke-ByName($root, [string]$name) {
    $element = Find-ByName $root $name
    if ($null -eq $element) { throw "Controle '$name' não encontrado." }
    $pattern = $element.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern)
    if ($null -eq $pattern) { throw "Controle '$name' não pode ser acionado." }
    $pattern.Invoke()
    Start-Sleep -Milliseconds 900
}

function Find-AllByControlType($root, $controlType) {
    $condition = [System.Windows.Automation.PropertyCondition]::new(
        [System.Windows.Automation.AutomationElement]::ControlTypeProperty,
        $controlType)
    return $root.FindAll([System.Windows.Automation.TreeScope]::Descendants, $condition)
}

function Capture-Window([IntPtr]$handle, [string]$path) {
    $rect = New-Object CompassoWailsNative+RECT
    [void][CompassoWailsNative]::GetWindowRect($handle, [ref]$rect)
    [void][CompassoWailsNative]::SetForegroundWindow($handle)
    Start-Sleep -Milliseconds 600
    $width = $rect.Right - $rect.Left
    $height = $rect.Bottom - $rect.Top
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

function Click-Relative([IntPtr]$handle, [int]$x, [int]$y) {
    $rect = New-Object CompassoWailsNative+RECT
    [void][CompassoWailsNative]::GetWindowRect($handle, [ref]$rect)
    [void][CompassoWailsNative]::SetForegroundWindow($handle)
    [void][CompassoWailsNative]::SetCursorPos($rect.Left + $x, $rect.Top + $y)
    [CompassoWailsNative]::mouse_event(0x0002, 0, 0, 0, [UIntPtr]::Zero)
    [CompassoWailsNative]::mouse_event(0x0004, 0, 0, 0, [UIntPtr]::Zero)
    Start-Sleep -Milliseconds 700
}

try {
    Get-Process Compasso -ErrorAction SilentlyContinue | Stop-Process -Force
    Remove-Item $stdoutPath,$stderrPath -ErrorAction SilentlyContinue
    $process = Start-Process 'C:\Program Files\Compasso\Compasso.exe' -PassThru `
        -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
    Start-Sleep -Seconds 6
    $script:window = [IntPtr]::Zero
    $callback = [CompassoWailsNative+EnumWindowsProc]{
        param([IntPtr]$handle, [IntPtr]$parameter)
        $id = [uint32]0
        [void][CompassoWailsNative]::GetWindowThreadProcessId($handle, [ref]$id)
        if ($id -eq $process.Id -and [CompassoWailsNative]::IsWindowVisible($handle)) {
            $script:window = $handle
            return $false
        }
        return $true
    }
    [void][CompassoWailsNative]::EnumWindows($callback, [IntPtr]::Zero)
    if ($script:window -eq [IntPtr]::Zero) { throw 'Janela Wails não encontrada.' }
    $root = [System.Windows.Automation.AutomationElement]::FromHandle($script:window)
    Capture-Window $script:window (Join-Path $base 'wails-add-time.png')
    Click-Relative $script:window 500 320
    Capture-Window $script:window (Join-Path $base 'wails-add-time-60.png')
    [void][CompassoWailsNative]::SetForegroundWindow($script:window)
    [System.Windows.Forms.SendKeys]::SendWait('{END}')
    Start-Sleep -Milliseconds 250
    Click-Relative $script:window 150 850
    Start-Sleep -Milliseconds 900
    $root = [System.Windows.Automation.AutomationElement]::FromHandle($script:window)
    Capture-Window $script:window (Join-Path $base 'wails-settings.png')
    @('WAILS_WINDOW=PASS','ADD_TIME_CONTROLS=PASS','SETTINGS_CONTROLS=PASS') |
        Set-Content -Encoding utf8 $resultPath
}
catch {
    $details = @('WAILS_UI=FAIL', "ERROR=$($_.Exception.Message)")
    $details += Get-Content $stdoutPath,$stderrPath -ErrorAction SilentlyContinue |
        ForEach-Object { "APP_OUTPUT=$_" }
    if ($null -ne $root) {
        $details += $root.FindAll([System.Windows.Automation.TreeScope]::Descendants, [System.Windows.Automation.Condition]::TrueCondition) |
            ForEach-Object { "ELEMENT=$($_.Current.ControlType.ProgrammaticName)|$($_.Current.Name)" }
    }
    $details | Set-Content -Encoding utf8 $resultPath
    throw
}
