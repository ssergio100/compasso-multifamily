//go:build windows

package alert

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsInvalidSessionID = ^uint32(0)
	windowsAlertSound       = 0x00000030                                  // MB_ICONWARNING
	windowsAlertStyle       = windowsAlertSound | 0x00010000 | 0x00040000 // MB_SETFOREGROUND | MB_TOPMOST
)

var (
	user32             = windows.NewLazySystemDLL("user32.dll")
	messageBeepProc    = user32.NewProc("MessageBeep")
	messageBoxProc     = user32.NewProc("MessageBoxW")
	windowsExecutable  = os.Executable
	launchAlertProcess = launchWindowsAlertProcess
	playAlertSound     = playWindowsAlertSound
	showAlertMessage   = showWindowsAlertMessage
)

// WindowsNotifier starts one short-lived presentation process in the active
// console session. The service remains responsible only for alert timing.
type WindowsNotifier struct {
	activeSessionID func() uint32
}

func NewWindowsNotifier() *WindowsNotifier {
	return &WindowsNotifier{activeSessionID: windows.WTSGetActiveConsoleSessionId}
}

func (notifier *WindowsNotifier) Notify(ctx context.Context, scheduledAlert Alert) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	message := strings.TrimSpace(scheduledAlert.Title)
	if notifier == nil || notifier.activeSessionID == nil || message == "" {
		return errors.New("Windows notifier and alert title are required")
	}
	sessionID := notifier.activeSessionID()
	if sessionID == windowsInvalidSessionID {
		return errors.New("active Windows console session is unavailable")
	}
	executablePath, err := windowsExecutable()
	if err != nil {
		return fmt.Errorf("locate Windows alert executable: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte(message))
	return launchAlertProcess(sessionID, executablePath, payload)
}

func launchWindowsAlertProcess(sessionID uint32, executablePath, payload string) error {
	var userToken windows.Token
	if err := windows.WTSQueryUserToken(sessionID, &userToken); err != nil {
		return fmt.Errorf("query active Windows session token: %w", err)
	}
	defer userToken.Close()

	var environment *uint16
	if err := windows.CreateEnvironmentBlock(&environment, userToken, false); err != nil {
		return fmt.Errorf("create Windows alert environment: %w", err)
	}
	defer windows.DestroyEnvironmentBlock(environment)

	application, err := windows.UTF16PtrFromString(executablePath)
	if err != nil {
		return fmt.Errorf("encode Windows alert executable: %w", err)
	}
	commandLine, err := windows.UTF16PtrFromString(
		windows.EscapeArg(executablePath) + " alert-ui " + windows.EscapeArg(payload),
	)
	if err != nil {
		return fmt.Errorf("encode Windows alert command: %w", err)
	}
	desktop, err := windows.UTF16PtrFromString(`winsta0\default`)
	if err != nil {
		return fmt.Errorf("encode Windows alert desktop: %w", err)
	}
	workingDirectory, err := windows.UTF16PtrFromString(filepath.Dir(executablePath))
	if err != nil {
		return fmt.Errorf("encode Windows alert working directory: %w", err)
	}
	startup := windows.StartupInfo{
		Cb:         uint32(unsafe.Sizeof(windows.StartupInfo{})),
		Desktop:    desktop,
		Flags:      windows.STARTF_USESHOWWINDOW,
		ShowWindow: windows.SW_SHOWNORMAL,
	}
	var process windows.ProcessInformation
	if err := windows.CreateProcessAsUser(
		userToken, application, commandLine, nil, nil, false,
		windows.CREATE_UNICODE_ENVIRONMENT|windows.CREATE_NO_WINDOW,
		environment, workingDirectory, &startup, &process,
	); err != nil {
		return fmt.Errorf("start Windows alert in active session: %w", err)
	}
	_ = windows.CloseHandle(process.Thread)
	_ = windows.CloseHandle(process.Process)
	return nil
}

// ShowWindowsAlert runs only in the short-lived process created in the user's
// session. It returns after the user acknowledges the message.
func ShowWindowsAlert(payload string) error {
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return fmt.Errorf("decode Windows alert: %w", err)
	}
	message := strings.TrimSpace(string(decoded))
	if message == "" {
		return errors.New("Windows alert message is required")
	}
	soundErr := playAlertSound()
	if err := showAlertMessage(message); err != nil {
		return err
	}
	return soundErr
}

func playWindowsAlertSound() error {
	if result, _, callErr := messageBeepProc.Call(windowsAlertSound); result == 0 {
		return fmt.Errorf("play Windows alert sound: %w", callErr)
	}
	return nil
}

func showWindowsAlertMessage(message string) error {
	titleUTF16, err := windows.UTF16FromString("Compasso")
	if err != nil {
		return fmt.Errorf("encode Windows alert title: %w", err)
	}
	messageUTF16, err := windows.UTF16FromString(message)
	if err != nil {
		return fmt.Errorf("encode Windows alert message: %w", err)
	}
	result, _, callErr := messageBoxProc.Call(
		0,
		uintptr(unsafe.Pointer(&messageUTF16[0])),
		uintptr(unsafe.Pointer(&titleUTF16[0])),
		windowsAlertStyle,
	)
	if result == 0 {
		return fmt.Errorf("display Windows alert: %w", callErr)
	}
	return nil
}
