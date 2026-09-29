import { useEffect, useState, type Dispatch, type RefObject, type SetStateAction } from "react";
import { api } from "../api";
import type { CommunicationLog, Device, DeviceActivity, LiveStatus, LiveStreamState } from "../types";

export function useDeviceStream({ deviceId, enabled, setDevices, pendingOperations, notify }: {
  deviceId?: string;
  enabled: boolean;
  setDevices: Dispatch<SetStateAction<Device[]>>;
  pendingOperations: RefObject<Set<string>>;
  notify: (text: string) => void;
}) {
  const [streamState, setStreamState] = useState<LiveStreamState>(enabled ? "connecting" : "live");
  const [streamGeneration, setStreamGeneration] = useState(0);
  const [activityUpdate, setActivityUpdate] = useState<DeviceActivity | null>(null);
  const [communicationUpdate, setCommunicationUpdate] = useState<CommunicationLog | null>(null);

  useEffect(() => {
    if (!enabled || !deviceId) {
      setStreamState("live");
      return;
    }
    setStreamState("connecting");
    setActivityUpdate(null);
    setCommunicationUpdate(null);
    const stream = api.openStream(deviceId);
    const onStatus = (event: MessageEvent) => {
      try {
        const status = JSON.parse(event.data) as LiveStatus;
        setDevices((all) => all.map((item) => item.id === deviceId ? {
          ...item,
          online: status.online,
          graphical_session_active: status.graphical_session_active,
          actual_state: status.actual_state,
          control_status: status.control_status,
          counting: status.counting,
          used_seconds: status.used_seconds,
          remaining_seconds: status.remaining_seconds,
          bonus_seconds: status.bonus_seconds,
          today_quota_seconds: status.today_quota_seconds,
          last_seen_at: status.online ? new Date().toISOString() : item.last_seen_at,
        } : item));
      } catch { /* ignora evento inválido */ }
    };
    stream.addEventListener("hello", onStatus);
    stream.addEventListener("status", onStatus);
    stream.addEventListener("device_offline", onStatus);
    const onActivity = (event: MessageEvent) => {
      try {
        const activity = JSON.parse(event.data) as DeviceActivity;
        setActivityUpdate(activity);
        if (activity.status === "completed" && pendingOperations.current.delete(activity.id)) {
          notify(activity.kind === "add_bonus" ? "Tempo extra aplicado e confirmado pelo computador." : "Alteração aplicada e confirmada pelo computador.");
        }
      } catch { /* ignora evento inválido */ }
    };
    stream.addEventListener("activity_updated", onActivity);
    stream.addEventListener("activities_changed", () => setStreamGeneration((generation) => generation + 1));
    stream.addEventListener("communication", (event) => {
      try { setCommunicationUpdate(JSON.parse((event as MessageEvent).data) as CommunicationLog); } catch { /* ignora evento inválido */ }
    });
    stream.onopen = () => {
      setStreamState("live");
      setStreamGeneration((generation) => generation + 1);
    };
    stream.onerror = () => setStreamState("interrupted");
    return () => stream.close();
  }, [deviceId, enabled, notify, pendingOperations, setDevices]);

  return { streamState, streamGeneration, activityUpdate, communicationUpdate };
}
