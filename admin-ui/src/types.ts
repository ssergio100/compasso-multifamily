export type View = "now" | "limits" | "routines" | "administration" | "communication";
export type AvatarKey = "capybara" | "cat" | "chick" | "dog" | "fox" | "lion" | "owl" | "panda" | "penguin" | "rabbit" | "sheep" | "tiger";
export type RoutineIconKey = "study" | "reading" | "sleep" | "bath" | "meal" | "school" | "exercise" | "chores" | "family" | "music" | "outdoor" | "general";
export type ActualState = "offline" | "blocked" | "unblocked";
export type ControlStatus = "offline" | "pause_requested" | "resume_requested" | "paused" | "block_requested" | "unblock_requested" | "blocked" | "active";

export type CommunicationParty = "agent" | "api" | "interface";
export type CommunicationResult = "success" | "warning" | "error";

export interface CommunicationLog {
  id: number;
  device_id: string;
  source: CommunicationParty;
  target: CommunicationParty;
  operation: string;
  result: CommunicationResult;
  http_status?: number;
  duration_ms?: number;
  summary: string;
  details: Record<string, string>;
  created_at: string;
}

export interface CommunicationResponse {
  events: CommunicationLog[];
  retention_days: number;
}

export type DeviceActivityStatus = "waiting_device" | "offered" | "completed" | "attention" | "failed";
export type DeviceActivityOrigin = "admin" | "device" | "server";
export type LiveStreamState = "connecting" | "live" | "interrupted";

export interface ActivityStep {
  kind: string;
  actor: DeviceActivityOrigin;
  occurred_at: string;
  last_occurred_at: string;
  occurrences: number;
  details: Record<string, string>;
}

export interface DeviceActivity {
  id: string;
  device_id: string;
  kind: string;
  origin: DeviceActivityOrigin;
  status: DeviceActivityStatus;
  details: Record<string, string>;
  occurred_at: string;
  observed_at: string;
  completed_at?: string;
  expires_at?: string;
  steps: ActivityStep[];
}

export interface DeviceActivitiesResponse { activities: DeviceActivity[] }

export interface Routine {
  id: string;
  name: string;
  days: [boolean, boolean, boolean, boolean, boolean, boolean, boolean];
  start_second: number;
  end_second: number;
  enabled: boolean;
  icon_key?: RoutineIconKey;
}

export interface Device {
  id: string;
  name: string;
  avatar_key?: AvatarKey;
  online: boolean;
  graphical_session_active: boolean;
  monitoring_paused: boolean;
  manual_block: boolean;
  actual_state: ActualState;
  control_status: ControlStatus;
  counting: boolean;
  used_seconds: number;
  remaining_seconds: number;
  bonus_seconds: number;
  today_quota_seconds: number;
  warning_minutes: number;
  last_seen_at: string | null;
  weekly_quota_seconds: number[];
  routines: Routine[];
  password_set: boolean;
}

export interface LiveStatus {
  local_date: string;
  today_quota_seconds: number;
  bonus_seconds: number;
  used_seconds: number;
  remaining_seconds: number;
  counting: boolean;
  online: boolean;
  graphical_session_active: boolean;
  actual_state: ActualState;
  control_status: ControlStatus;
}

export interface Session { authenticated: boolean; login?: string; csrf_token: string; setup_required: boolean; email_required?: boolean }

export interface DeviceResponse {
  id: string; name: string; avatar_key?: AvatarKey; last_seen_at: string | null; policy_revision: number; applied_policy_revision: number;
  graphical_session_active: boolean; online: boolean;
}

export interface DeviceDetailResponse {
  device: DeviceResponse;
  policy: { revision: number; monitoring_paused: boolean; manual_block: boolean; warning_minutes: number; password_set: boolean; weekly_quota_seconds: number[]; routines: Routine[] };
  control: { revision: number; monitoring_paused: boolean; manual_block: boolean };
  status: { today_quota_seconds: number; bonus_seconds: number; used_seconds: number; remaining_seconds: number; counting: boolean; online: boolean; graphical_session_active: boolean; actual_state: ActualState; control_status: ControlStatus };
}
