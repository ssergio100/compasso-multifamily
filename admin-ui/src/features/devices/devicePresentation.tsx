import { LockKeyhole, MonitorCheck, Pause, RefreshCw, ShieldOff } from "lucide-react";
import type { AvatarKey, Device } from "../../types";
import { defaultAvatarKey } from "../../visuals";

export function deviceVisualState(device: Device) {
  if (!device.online) return "offline";
  if (["block_requested", "unblock_requested", "pause_requested", "resume_requested"].includes(device.control_status)) return "pending";
  if (device.control_status === "paused") return "paused";
  if (device.actual_state === "blocked" || device.control_status === "blocked") return "blocked";
  return "online";
}

export function avatarKeyFor(device: Device): AvatarKey {
  return device.avatar_key ?? defaultAvatarKey(device.id);
}

export function deviceIsBlockedForAction(device: Device) {
  if (device.control_status === "unblock_requested") return false;
  return device.manual_block || device.actual_state === "blocked" || device.control_status === "blocked";
}

export function DeviceState({ device }: { device: Device }) {
  const state = deviceVisualState(device);
  const Icon = state === "blocked" ? LockKeyhole : state === "paused" ? Pause : state === "pending" ? RefreshCw : state === "online" ? MonitorCheck : ShieldOff;
  const onlineDetail = device.control_status === "block_requested" ? "Bloqueando…"
    : device.control_status === "unblock_requested" ? "Desbloqueando…"
      : device.control_status === "pause_requested" ? "Pausando…"
        : device.control_status === "resume_requested" ? "Retomando…"
          : state === "blocked" ? "Bloqueado"
            : state === "paused" ? "Pausado"
              : "";
  return <small className={`device-state ${state}`}><Icon aria-hidden="true" size={13} /><span className="connectivity-state">{device.online ? "Online" : "Offline"}</span>{device.online && onlineDetail && <><span aria-hidden="true">—</span><span>{onlineDetail}</span></>}</small>;
}
