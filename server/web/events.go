package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ssergio100/compasso/server/storage"
)

const streamKeepAliveInterval = 15 * time.Second

type streamEvent struct {
	Name string          `json:"-"`
	Data json.RawMessage `json:"data"`
}

// eventHub delivers device updates to subscribed browser streams. It never
// blocks publishers: slow or disconnected subscribers are dropped.
type eventHub struct {
	mu          sync.Mutex
	subscribers map[string]map[chan streamEvent]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{subscribers: make(map[string]map[chan streamEvent]struct{})}
}

func (h *eventHub) subscribe(deviceID string) (chan streamEvent, func()) {
	events := make(chan streamEvent, 8)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subscribers[deviceID] == nil {
		h.subscribers[deviceID] = make(map[chan streamEvent]struct{})
	}
	h.subscribers[deviceID][events] = struct{}{}
	return events, func() { h.unsubscribe(deviceID, events) }
}

func (h *eventHub) unsubscribe(deviceID string, channel chan streamEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if subscribers := h.subscribers[deviceID]; subscribers != nil {
		if _, present := subscribers[channel]; present {
			delete(subscribers, channel)
			close(channel)
		}
		if len(subscribers) == 0 {
			delete(h.subscribers, deviceID)
		}
	}
}

func (h *eventHub) publish(deviceID string, event streamEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for channel := range h.subscribers[deviceID] {
		select {
		case channel <- event:
		default:
		}
	}
}

func (h *eventHub) hasSubscribers(deviceID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subscribers[deviceID]) > 0
}

// adminDeviceStreamAPI opens a Server-Sent Events stream for a device. It
// sends the current status snapshot as "hello", then live "status" snapshots
// on every heartbeat and a "device_offline" snapshot when the online timeout
// expires. The admin session is validated when the stream is opened and on
// every keep-alive.
func (a *App) adminDeviceStreamAPI(w http.ResponseWriter, r *http.Request, device storage.FamilyDevice) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _, liveStatus, err := a.loadAdministrativeDeviceLiveStatus(r.Context(), device)
	if !writeAdminReadError(w, err) {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	events, unsubscribe := a.hub.subscribe(device.ID())
	defer unsubscribe()
	if err := a.writeStreamSnapshot(w, flusher, "hello", liveStatus); err != nil {
		return
	}
	keepAlive := time.NewTicker(streamKeepAliveInterval)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-events:
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Name, event.Data); err != nil {
				return
			}
			flusher.Flush()
		case <-keepAlive.C:
			if _, _, authenticated := a.authenticated(r); !authenticated {
				return
			}
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (a *App) writeStreamSnapshot(w http.ResponseWriter, flusher http.Flusher, name string, status deviceLiveStatus) error {
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func (a *App) publishDeviceStatus(deviceID string, name string) {
	if !a.hub.hasSubscribers(deviceID) {
		return
	}
	_, _, liveStatus, err := a.loadDeviceLiveStatus(context.Background(), deviceID)
	if err != nil {
		return
	}
	data, err := json.Marshal(liveStatus)
	if err != nil {
		return
	}
	a.hub.publish(deviceID, streamEvent{Name: name, Data: data})
}

// publishCommunicationLog forwards a stored communication log to subscribers
// of the device stream, so the communication view updates without polling.
func (a *App) publishCommunicationLog(deviceID string, log storage.CommunicationLog) {
	if !a.hub.hasSubscribers(deviceID) {
		return
	}
	data, err := json.Marshal(log)
	if err != nil {
		return
	}
	a.hub.publish(deviceID, streamEvent{Name: "communication", Data: data})
}

func (a *App) publishAdministrativeCommunicationLog(device storage.FamilyDevice, log storage.CommunicationLog) {
	if !a.hub.hasSubscribers(device.ID()) {
		return
	}
	data, err := json.Marshal(log)
	if err != nil {
		return
	}
	a.hub.publish(device.ID(), streamEvent{Name: "communication", Data: data})
}

func (a *App) publishDeviceActivity(deviceID, activityID string) {
	if !a.hub.hasSubscribers(deviceID) {
		return
	}
	activity, err := a.store.LoadDeviceActivity(context.Background(), deviceID, activityID)
	if err != nil {
		return
	}
	a.publishActivity(activity)
}

func (a *App) publishAdministrativeDeviceActivity(device storage.FamilyDevice, activityID string) {
	if !a.hub.hasSubscribers(device.ID()) {
		return
	}
	activity, err := device.LoadActivity(context.Background(), activityID)
	if err != nil {
		return
	}
	a.publishActivity(activity)
}

func (a *App) publishActivity(activity storage.DeviceActivity) {
	if !a.hub.hasSubscribers(activity.DeviceID) {
		return
	}
	data, err := json.Marshal(activity)
	if err != nil {
		return
	}
	a.hub.publish(activity.DeviceID, streamEvent{Name: "activity_updated", Data: data})
}

// publishActivitiesChanged asks the active administrative view to reload the
// durable activity list after a successful mutation. It complements the
// detailed activity_updated event and closes the race where that event could
// arrive just before an older screen-loading request completed.
func (a *App) publishActivitiesChanged(deviceID string) {
	if !a.hub.hasSubscribers(deviceID) {
		return
	}
	data, err := json.Marshal(map[string]string{"device_id": deviceID})
	if err != nil {
		return
	}
	a.hub.publish(deviceID, streamEvent{Name: "activities_changed", Data: data})
}

func (a *App) publishAdministrativeActivitiesChanged(device storage.FamilyDevice) {
	if !a.hub.hasSubscribers(device.ID()) {
		return
	}
	data, err := json.Marshal(map[string]string{"device_id": device.ID()})
	if err != nil {
		return
	}
	a.hub.publish(device.ID(), streamEvent{Name: "activities_changed", Data: data})
}

// StartOfflineDetector publishes "device_offline" when the online timeout
// expires without heartbeats. Heartbeats publishing "status" naturally reset
// the offline state in the panel.
func (a *App) StartOfflineDetector(ctx context.Context) {
	go a.runOfflineDetector(ctx)
}

func (a *App) runOfflineDetector(ctx context.Context) {
	interval := a.onlineTimeout / 2
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var publishedOffline sync.Map
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := a.now()
			devices, err := a.store.ListDevices(ctx)
			if err != nil {
				continue
			}
			for _, device := range devices {
				if !a.hub.hasSubscribers(device.ID) {
					publishedOffline.Delete(device.ID)
					continue
				}
				if isOnline(device.LastSeenAt, now, a.onlineTimeout) {
					publishedOffline.Delete(device.ID)
					continue
				}
				if _, already := publishedOffline.LoadOrStore(device.ID, struct{}{}); already {
					continue
				}
				a.publishDeviceStatus(device.ID, "device_offline")
			}
		}
	}
}
