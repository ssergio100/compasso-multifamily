<# Valida a escolha exclusiva dos períodos pelo mesmo clique usado pelo usuário. #>
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class AddTimeSelectionNative {
    public delegate bool EnumWindowsProc(IntPtr hWnd, IntPtr lParam);
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
    [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc callback, IntPtr lParam);
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint processId);
    [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);
    [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern void mouse_event(uint flags, uint dx, uint dy, uint data, UIntPtr extraInfo);
}
"@

$resultPath = 'C:\CompassoWinUIBootstrap\add-time-selection-result.txt'
$screenshotPath = 'C:\CompassoWinUIBootstrap\add-time-selection-15.png'

function Find-ByAutomationId([System.Windows.Automation.AutomationElement]$root, [string]$id) {
    $condition = [System.Windows.Automation.PropertyCondition]::new(
        [System.Windows.Automation.AutomationElement]::AutomationIdProperty,
        $id)
    return $root.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $condition)
}

function Test-IsSelected([System.Windows.Automation.AutomationElement]$element) {
    $pattern = $null
    if ($element.TryGetCurrentPattern([System.Windows.Automation.SelectionItemPattern]::Pattern, [ref]$pattern)) {
        return ([System.Windows.Automation.SelectionItemPattern]$pattern).Current.IsSelected
    }
    if ($element.TryGetCurrentPattern([System.Windows.Automation.TogglePattern]::Pattern, [ref]$pattern)) {
        return ([System.Windows.Automation.TogglePattern]$pattern).Current.ToggleState -eq [System.Windows.Automation.ToggleState]::On
    }
    throw "O controle $($element.Current.AutomationId) não expõe um padrão de seleção."
}

function Click-Element([System.Windows.Automation.AutomationElement]$element) {
    $bounds = $element.Current.BoundingRectangle
    $x = [int]($bounds.X + $bounds.Width / 2)
    $y = [int]($bounds.Y + $bounds.Height / 2)
    [void][AddTimeSelectionNative]::SetCursorPos($x, $y)
    Start-Sleep -Milliseconds 250
    [AddTimeSelectionNative]::mouse_event(0x0002, 0, 0, 0, [UIntPtr]::Zero)
    [AddTimeSelectionNative]::mouse_event(0x0004, 0, 0, 0, [UIntPtr]::Zero)
    Start-Sleep -Milliseconds 700
}

function Assert-Selection([hashtable]$periods, [string]$expected, [System.Windows.Automation.AutomationElement]$confirmButton) {
    $selected = @($periods.Keys | Where-Object { Test-IsSelected $periods[$_] })
    if ($selected.Count -ne 1 -or $selected[0] -ne $expected) {
        throw "Seleção inválida: esperado=$expected; selecionados=$($selected -join ',')."
    }
    $expectedLabel = "Adicionar $expected minutos"
    if ($confirmButton.Current.Name -ne $expectedLabel) {
        throw "Texto da ação inválido: esperado='$expectedLabel'; atual='$($confirmButton.Current.Name)'."
    }
    return "$expected=PASS"
}

try {
    Get-Process CompassoApp -ErrorAction SilentlyContinue | Stop-Process -Force
    $process = Start-Process 'C:\Program Files\Compasso\CompassoApp.exe' -PassThru
    Start-Sleep -Seconds 7

    $script:window = [IntPtr]::Zero
    $callback = [AddTimeSelectionNative+EnumWindowsProc]{
        param([IntPtr]$handle, [IntPtr]$parameter)
        $id = [uint32]0
        [void][AddTimeSelectionNative]::GetWindowThreadProcessId($handle, [ref]$id)
        if ($id -eq $process.Id -and [AddTimeSelectionNative]::IsWindowVisible($handle)) {
            $script:window = $handle
            return $false
        }
        return $true
    }
    [void][AddTimeSelectionNative]::EnumWindows($callback, [IntPtr]::Zero)
    if ($script:window -eq [IntPtr]::Zero) { throw 'Janela do Compasso não encontrada.' }
    [void][AddTimeSelectionNative]::SetForegroundWindow($script:window)

    $root = [System.Windows.Automation.AutomationElement]::FromHandle($script:window)
    $periods = @{}
    foreach ($minutes in @('15', '30', '60', '120')) {
        $periods[$minutes] = Find-ByAutomationId $root "Duration$minutes"
        if ($null -eq $periods[$minutes]) { throw "Período $minutes não encontrado." }
    }
    $confirmButton = Find-ByAutomationId $root 'AddTimeButton'
    if ($null -eq $confirmButton) { throw 'Ação principal não encontrada.' }

    $results = @()
    $results += Assert-Selection $periods '30' $confirmButton
    Click-Element $periods['60']
    $results += Assert-Selection $periods '60' $confirmButton
    Click-Element $periods['15']
    $results += Assert-Selection $periods '15' $confirmButton

    $rect = New-Object AddTimeSelectionNative+RECT
    [void][AddTimeSelectionNative]::GetWindowRect($script:window, [ref]$rect)
    $width = $rect.Right - $rect.Left
    $height = $rect.Bottom - $rect.Top
    $bitmap = [System.Drawing.Bitmap]::new($width, $height)
    $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
    try {
        $graphics.CopyFromScreen($rect.Left, $rect.Top, 0, 0, [System.Drawing.Size]::new($width, $height))
        $bitmap.Save($screenshotPath, [System.Drawing.Imaging.ImageFormat]::Png)
    }
    finally {
        $graphics.Dispose()
        $bitmap.Dispose()
    }

    @(
        'EXCLUSIVE_SELECTION=PASS'
        'FLOW=30->60->15'
        $results
        "PROCESS_ID=$($process.Id)"
    ) | Set-Content -Encoding utf8 $resultPath
}
catch {
    @(
        'EXCLUSIVE_SELECTION=FAIL'
        "ERROR=$($_.Exception.Message)"
    ) | Set-Content -Encoding utf8 $resultPath
    throw
}
