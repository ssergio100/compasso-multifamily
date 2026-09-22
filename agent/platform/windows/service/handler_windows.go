//go:build windows
// +build windows

package service

import (
	"context"
	"time"
	"unsafe"

	windowsession "github.com/ssergio100/compasso/agent/platform/windows/session"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

const acceptedCommands = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptSessionChange

type consoleObserver interface {
	ActiveConsole(context.Context) (*windowsession.Snapshot, error)
}

type sessionLifecycle interface {
	Reconcile(context.Context) error
	Stop()
}

type agentRuntime interface {
	Run(context.Context) error
}

// Handler connects SCM session-change events to the platform-neutral Windows
// session model. Policy and enforcement deliberately do not live here.
type Handler struct {
	observer  consoleObserver
	sink      SessionSink
	locks     *windowsession.LockTracker
	now       func() time.Time
	lifecycle sessionLifecycle
	runtime   agentRuntime
}

func NewHandler(observer consoleObserver, sink SessionSink) *Handler {
	return &Handler{observer: observer, sink: sink, locks: windowsession.NewLockTracker(), now: time.Now}
}

func (h *Handler) WithLockTracker(locks *windowsession.LockTracker) *Handler {
	if locks != nil {
		h.locks = locks
	}
	return h
}

func (h *Handler) WithSessionLifecycle(lifecycle sessionLifecycle) *Handler {
	h.lifecycle = lifecycle
	return h
}

func (h *Handler) WithRuntime(runtime agentRuntime) *Handler {
	h.runtime = runtime
	return h
}

func (h *Handler) Execute(_ []string, requests <-chan svc.ChangeRequest, statuses chan<- svc.Status) (bool, uint32) {
	statuses <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err := h.recordInitialState(ctx)
	cancel()
	if err != nil {
		statuses <- svc.Status{State: svc.StopPending}
		return true, 1
	}
	if h.lifecycle != nil {
		defer h.lifecycle.Stop()
		ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
		err = h.lifecycle.Reconcile(ctx)
		cancel()
		if err != nil {
			statuses <- svc.Status{State: svc.StopPending}
			return true, 1
		}
	}
	current := svc.Status{State: svc.Running, Accepts: acceptedCommands}
	statuses <- current
	runtimeContext, stopRuntime := context.WithCancel(context.Background())
	defer stopRuntime()
	var runtimeDone <-chan error
	if h.runtime != nil {
		completed := make(chan error, 1)
		runtimeDone = completed
		go func() { completed <- h.runtime.Run(runtimeContext) }()
	}

	for {
		select {
		case runtimeError := <-runtimeDone:
			statuses <- svc.Status{State: svc.StopPending}
			if runtimeError != nil {
				return true, 1
			}
			return true, 1
		case request, open := <-requests:
			if !open {
				return h.stopRuntime(stopRuntime, runtimeDone)
			}
			switch request.Cmd {
			case svc.Interrogate:
				statuses <- current
			case svc.Stop, svc.Shutdown:
				statuses <- svc.Status{State: svc.StopPending}
				return h.stopRuntime(stopRuntime, runtimeDone)
			case svc.SessionChange:
				if err := h.recordSessionChange(request); err != nil {
					statuses <- svc.Status{State: svc.StopPending}
					_, _ = h.stopRuntime(stopRuntime, runtimeDone)
					return true, 1
				}
			}
		}
	}
}

func (h *Handler) stopRuntime(stop context.CancelFunc, completed <-chan error) (bool, uint32) {
	stop()
	if completed == nil {
		return false, 0
	}
	select {
	case err := <-completed:
		if err != nil {
			return true, 1
		}
		return false, 0
	case <-time.After(10 * time.Second):
		return true, 1
	}
}

func (h *Handler) recordInitialState(ctx context.Context) error {
	console, err := h.observer.ActiveConsole(ctx)
	if err != nil {
		return err
	}
	if console == nil {
		return nil
	}
	reconcileConsoleLock(h.locks, console)
	return h.sink.Record(SessionRecord{
		Event: windowsession.Event{
			Kind: windowsession.EventInitial, SessionID: console.SessionID, ObservedAt: h.now().UTC(),
		},
		Console: console,
	})
}

func (h *Handler) recordSessionChange(request svc.ChangeRequest) error {
	notification := (*windows.WTSSESSION_NOTIFICATION)(unsafe.Pointer(request.EventData))
	if notification == nil || notification.Size < uint32(unsafe.Sizeof(*notification)) {
		return nil
	}
	event, found := windowsession.DecodeEvent(request.EventType, notification.SessionID, h.now().UTC())
	if !found {
		return nil
	}
	h.locks.Apply(event)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	console, err := h.observer.ActiveConsole(ctx)
	if err != nil {
		return err
	}
	if console != nil {
		reconcileConsoleLock(h.locks, console)
	}
	if err := h.sink.Record(SessionRecord{Event: event, Console: console}); err != nil {
		return err
	}
	if h.lifecycle != nil {
		return h.lifecycle.Reconcile(ctx)
	}
	return nil
}
