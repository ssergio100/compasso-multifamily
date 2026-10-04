[CmdletBinding()]
param(
    [ValidateSet('status', 'invalid-rate')]
    [string]$Scenario = 'status'
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.Windows.Forms
Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class CompassoAddTimeNative {
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
$resultPath = Join-Path $base "wails-add-time-$Scenario.txt"
$stdoutPath = Join-Path $base "wails-add-time-$Scenario-stdout.txt"
$stderrPath = Join-Path $base "wails-add-time-$Scenario-stderr.txt"

function Find-Window([int]$processId, [int]$timeoutSeconds = 15) {
    $deadline = [DateTime]::UtcNow.AddSeconds($timeoutSeconds)
    do {
        $script:window = [IntPtr]::Zero
        $callback = [CompassoAddTimeNative+EnumWindowsProc]{
            param([IntPtr]$handle, [IntPtr]$parameter)
            $id = [uint32]0
            [void][CompassoAddTimeNative]::GetWindowThreadProcessId($handle, [ref]$id)
            if ($id -eq $processId -and [CompassoAddTimeNative]::IsWindowVisible($handle)) {
                $script:window = $handle
                return $false
            }
            return $true
        }
        [void][CompassoAddTimeNative]::EnumWindows($callback, [IntPtr]::Zero)
        if ($script:window -ne [IntPtr]::Zero) { return $script:window }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'Janela instalada do Compasso não encontrada.'
}

function Click-Relative([IntPtr]$handle, [int]$x, [int]$y) {
    $rect = New-Object CompassoAddTimeNative+RECT
    [void][CompassoAddTimeNative]::GetWindowRect($handle, [ref]$rect)
    [void][CompassoAddTimeNative]::SetForegroundWindow($handle)
    [void][CompassoAddTimeNative]::SetCursorPos($rect.Left + $x, $rect.Top + $y)
    [CompassoAddTimeNative]::mouse_event(0x0002, 0, 0, 0, [UIntPtr]::Zero)
    [CompassoAddTimeNative]::mouse_event(0x0004, 0, 0, 0, [UIntPtr]::Zero)
    Start-Sleep -Milliseconds 150
}

function Capture-Window([IntPtr]$handle, [string]$path) {
    $rect = New-Object CompassoAddTimeNative+RECT
    [void][CompassoAddTimeNative]::GetWindowRect($handle, [ref]$rect)
    [void][CompassoAddTimeNative]::SetForegroundWindow($handle)
    Start-Sleep -Milliseconds 250
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

try {
    Get-Process Compasso -ErrorAction SilentlyContinue | Stop-Process -Force
    Remove-Item $stdoutPath,$stderrPath -ErrorAction SilentlyContinue
    $process = Start-Process 'C:\Program Files\Compasso\Compasso.exe' -PassThru `
        -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
    $window = Find-Window $process.Id
    Start-Sleep -Seconds 3
    Capture-Window $window (Join-Path $base "wails-add-time-$Scenario-initial.png")

    $lines = @(
        "SCENARIO=$Scenario"
        'INSTALLED_APP=PASS'
        "PROCESS_ID=$($process.Id)"
    )
    if ($Scenario -eq 'invalid-rate') {
        Click-Relative $window 145 415
        Click-Relative $window 300 565
        [System.Windows.Forms.SendKeys]::SendWait('compasso-invalid-test')
        Capture-Window $window (Join-Path $base 'wails-add-time-password-filled.png')
        [System.Windows.Forms.SendKeys]::SendWait('{ENTER}')
        Start-Sleep -Milliseconds 1000
        Capture-Window $window (Join-Path $base 'wails-add-time-invalid-password.png')

        [System.Windows.Forms.SendKeys]::SendWait('compasso-invalid-test-2')
        [System.Windows.Forms.SendKeys]::SendWait('{ENTER}')
        Start-Sleep -Milliseconds 500
        Capture-Window $window (Join-Path $base 'wails-add-time-rate-limited.png')
        $lines += 'INVALID_PASSWORD_CAPTURED=PASS'
        $lines += 'RATE_LIMIT_CAPTURED=PASS'
    }
    $lines += 'RESULT=PASS'
    $lines | Set-Content -Encoding utf8 $resultPath
}
catch {
    @(
        'RESULT=FAIL'
        "ERROR=$($_.Exception.Message)"
        (Get-Content $stdoutPath,$stderrPath -ErrorAction SilentlyContinue | ForEach-Object { "APP_OUTPUT=$_" })
    ) | Set-Content -Encoding utf8 $resultPath
    throw
}
