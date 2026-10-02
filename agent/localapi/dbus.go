// Package localapi exposes the small local agent API on the system D-Bus.
package localapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/ssergio100/compasso/agent/localauth"
	"github.com/ssergio100/compasso/agent/localmsg"
)

// Local D-Bus error names kept for clients built against older packages. The
// wording now lives in agent/localmsg so Linux and Windows stay identical.
const (
	ErrorPasswordNotConfigured = "Error.PasswordNotConfigured"
	ErrorInvalidPassword       = "Error.InvalidPassword"
	ErrorRateLimited           = "Error.RateLimited"
	ErrorFailed                = "Error.Failed"
)

const (
	BusName       = "br.com.tempo.Agent"
	ObjectPath    = dbus.ObjectPath("/br/com/tempo/Agent")
	InterfaceName = "br.com.tempo.Agent"
)

// bonusAPI translates D-Bus requests into the local bonus service.
type bonusAPI struct {
	service         *localauth.Service
	synchronization synchronizationSource
	now             func() time.Time
}

type synchronizationSource interface {
	SynchronizationStatus() (checked, online bool)
}

type synchronizationReportSource interface {
	SynchronizationReport() (checked, online bool, detail string)
}

func newBonusAPI(service *localauth.Service, synchronization synchronizationSource) (*bonusAPI, error) {
	if service == nil {
		return nil, errors.New("local bonus service is required")
	}
	if synchronization == nil {
		return nil, errors.New("synchronization source is required")
	}
	return &bonusAPI{service: service, synchronization: synchronization, now: time.Now}, nil
}

// GetSynchronizationStatus returns the live heartbeat state without exposing
// credentials or connection error details.
func (a *bonusAPI) GetSynchronizationStatus() (string, *dbus.Error) {
	checked, online := a.synchronization.SynchronizationStatus()
	if !checked {
		return "checking", nil
	}
	if online {
		return "online", nil
	}
	return "offline", nil
}

// GetSynchronizationReport is the human-facing, additive successor to
// GetSynchronizationStatus. The old method remains available to clients from
// older packages, while new interfaces receive an actionable explanation.
func (a *bonusAPI) GetSynchronizationReport() (string, string, *dbus.Error) {
	checked, online := a.synchronization.SynchronizationStatus()
	detail := ""
	if source, ok := a.synchronization.(synchronizationReportSource); ok {
		checked, online, detail = source.SynchronizationReport()
	}
	status, human := localmsg.Report(synchronizationState(checked, online), detail)
	return status, human, nil
}

func synchronizationState(checked, online bool) string {
	switch {
	case !checked:
		return localmsg.StatusChecking
	case online:
		return localmsg.StatusOnline
	default:
		return localmsg.StatusOffline
	}
}

// AddLocalBonus is called by the unprivileged GTK dialog. The password is
// checked by the root agent and is never written to disk or logs.
func (a *bonusAPI) AddLocalBonus(password string, seconds uint32) (string, *dbus.Error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := a.service.Grant(ctx, password, int64(seconds), a.now())
	if err != nil {
		code := localauthErrorCode(err)
		return "", dbus.NewError(BusName+"."+dbusErrorName(code), []interface{}{localmsg.BonusError(code)})
	}
	return result.UUID, nil
}

// dbusErrorName preserves the historical D-Bus error names, which are part of
// the Linux interface contract, while the wording comes from agent/localmsg.
func dbusErrorName(code string) string {
	switch code {
	case localmsg.ErrorPasswordNotConfigured:
		return ErrorPasswordNotConfigured
	case localmsg.ErrorInvalidPassword:
		return ErrorInvalidPassword
	case localmsg.ErrorRateLimited:
		return ErrorRateLimited
	default:
		return ErrorFailed
	}
}

// localauthErrorCode maps the bonus service failures to the stable identifiers
// shared with the Windows named pipe.
func localauthErrorCode(err error) string {
	switch {
	case errors.Is(err, localauth.ErrPasswordNotConfigured):
		return localmsg.ErrorPasswordNotConfigured
	case errors.Is(err, localauth.ErrInvalidPassword):
		return localmsg.ErrorInvalidPassword
	case errors.Is(err, localauth.ErrRateLimited):
		return localmsg.ErrorRateLimited
	default:
		return localmsg.ErrorFailed
	}
}

// Server owns the system-bus connection and exported object.
type Server struct {
	connection *dbus.Conn
}

func ExportSystem(service *localauth.Service, synchronization synchronizationSource) (*Server, error) {
	api, err := newBonusAPI(service, synchronization)
	if err != nil {
		return nil, err
	}
	connection, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect system D-Bus: %w", err)
	}
	cleanup := func() { _ = connection.Close() }
	if err := connection.Export(api, ObjectPath, InterfaceName); err != nil {
		cleanup()
		return nil, fmt.Errorf("export local bonus API: %w", err)
	}
	node := &introspect.Node{
		Name: string(ObjectPath),
		Interfaces: []introspect.Interface{{
			Name: InterfaceName,
			Methods: []introspect.Method{{
				Name: "AddLocalBonus",
				Args: []introspect.Arg{
					{Name: "password", Type: "s", Direction: "in"},
					{Name: "seconds", Type: "u", Direction: "in"},
					{Name: "event_uuid", Type: "s", Direction: "out"},
				},
			}, {
				Name: "GetSynchronizationStatus",
				Args: []introspect.Arg{
					{Name: "status", Type: "s", Direction: "out"},
				},
			}, {
				Name: "GetSynchronizationReport",
				Args: []introspect.Arg{
					{Name: "status", Type: "s", Direction: "out"},
					{Name: "detail", Type: "s", Direction: "out"},
				},
			}},
		}, introspect.IntrospectData},
	}
	if err := connection.Export(introspect.NewIntrospectable(node), ObjectPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		cleanup()
		return nil, fmt.Errorf("export local API introspection: %w", err)
	}
	reply, err := connection.RequestName(BusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("request local agent D-Bus name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		cleanup()
		return nil, fmt.Errorf("D-Bus name %s is already owned", BusName)
	}
	return &Server{connection: connection}, nil
}

func (s *Server) Close() error {
	if s == nil || s.connection == nil {
		return nil
	}
	return s.connection.Close()
}
