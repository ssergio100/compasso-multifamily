import type { AvatarKey, CommunicationResponse, Device, DeviceActivitiesResponse, DeviceDetailResponse, DeviceResponse, Routine, Session } from "./types";
import { inferRoutineIcon, isRoutineIconKey, normalizeAvatarKey } from "./visuals";

const productionAPIBase = import.meta.env.PROD
  ? `${window.location.protocol}//${window.location.hostname}:8181`
  : "";
const runtimeBase = (window.COMPASSO_CONFIG?.apiBaseUrl ?? window.COMPASSO_CONFIG?.apiBaseURL ?? "").trim();
const envBase = (import.meta.env.VITE_COMPASSO_API_BASE_URL ?? "").trim();
const visualPreview = import.meta.env.DEV && new URLSearchParams(window.location.search).get("preview") === "visuals";
export const remoteMode = !visualPreview && (import.meta.env.VITE_COMPASSO_REMOTE === "true" || Boolean(runtimeBase || envBase || productionAPIBase));

function normalizedDays(value: unknown): Routine["days"] {
  const days = Array.isArray(value) ? value : [];
  return Array.from({ length: 7 }, (_, index) => Boolean(days[index])) as Routine["days"];
}

function normalizedRoutines(value: unknown): Routine[] {
  if (!Array.isArray(value)) return [];
  return value.filter((routine): routine is Routine => Boolean(routine && typeof routine === "object")).map((routine) => {
    const name = String(routine.name ?? "Rotina");
    return {
      id: String(routine.id ?? ""), name, days: normalizedDays(routine.days),
      start_second: Number.isFinite(Number(routine.start_second)) ? Number(routine.start_second) : 0,
      end_second: Number.isFinite(Number(routine.end_second)) ? Number(routine.end_second) : 0,
      enabled: Boolean(routine.enabled), icon_key: isRoutineIconKey(routine.icon_key) ? routine.icon_key : inferRoutineIcon(name),
    };
  });
}

function normalizedWeeklyQuota(value: unknown): number[] {
  const quotas = Array.isArray(value) ? value : [];
  return Array.from({ length: 7 }, (_, index) => Number.isFinite(Number(quotas[index])) ? Number(quotas[index]) : 0);
}

class API {
  private csrf = "";
  private base = import.meta.env.DEV ? "" : runtimeBase || envBase || productionAPIBase;

  private async request<T>(path: string, init: RequestInit = {}, mutate = false): Promise<T> {
    const headers = new Headers(init.headers);
    headers.set("Accept", "application/json");
    if (init.body) headers.set("Content-Type", "application/json");
    if (mutate && this.csrf) headers.set("X-CSRF-Token", this.csrf);
    const response = await fetch(`${this.base}${path}`, { ...init, headers, credentials: "include", cache: "no-store" });
    if (response.status === 204) return undefined as T;
    const isJSON = response.headers.get("Content-Type")?.includes("application/json") ?? false;
    const payload = isJSON ? await response.json().catch(() => ({})) : {};
    if (!response.ok) {
      const wrongServer = response.status === 405 && !isJSON;
      throw new Error(wrongServer
        ? "A interface está apontando para o servidor de arquivos, não para a API. Revise runtime-config.js."
        : payload.error ?? "Não foi possível concluir a operação.");
    }
    return payload as T;
  }

  async session() { const value = await this.request<Session>("/api/v1/admin/session"); this.csrf = value.csrf_token; return value; }
  async login(login: string, password: string) {
    if (!this.csrf) await this.session();
    const value = await this.request<Session>("/api/v1/admin/session", { method: "POST", body: JSON.stringify({ login, password, csrf_token: this.csrf }) });
    this.csrf = value.csrf_token; return value;
  }
  async logout() { await this.request<void>("/api/v1/admin/session", { method: "DELETE" }, true); }
  async register(familyName: string, email: string, password: string, confirmation: string) {
    if (!this.csrf) await this.session();
    return this.request<{ message: string }>("/api/v1/account/register", { method: "POST", body: JSON.stringify({ family_name: familyName, email, password, password_confirmation: confirmation, csrf_token: this.csrf }) });
  }
  async resendConfirmation(email: string) {
    if (!this.csrf) await this.session();
    return this.request<{ message: string }>("/api/v1/account/resend-confirmation", { method: "POST", body: JSON.stringify({ email, csrf_token: this.csrf }) });
  }
  async requestPasswordReset(email: string) {
    if (!this.csrf) await this.session();
    return this.request<{ message: string }>("/api/v1/account/password-reset", { method: "POST", body: JSON.stringify({ email, csrf_token: this.csrf }) });
  }
  confirmRegistration(token: string) { return this.request<{ message: string }>("/api/v1/account/confirm", { method: "POST", body: JSON.stringify({ token }) }); }
  confirmPasswordReset(token: string, password: string, confirmation: string) { return this.request<{ message: string }>("/api/v1/account/password-reset/confirm", { method: "POST", body: JSON.stringify({ token, password, password_confirmation: confirmation }) }); }
  confirmEmailChange(token: string) { return this.request<{ message: string }>("/api/v1/account/email/confirm", { method: "POST", body: JSON.stringify({ token }) }); }
  account() { return this.request<{ email: string; family_name: string }>("/api/v1/admin/account"); }
  changeAccountPassword(currentPassword: string, password: string, confirmation: string) { return this.request<{ message: string }>("/api/v1/admin/account/password", { method: "PUT", body: JSON.stringify({ current_password: currentPassword, password, password_confirmation: confirmation }) }, true); }
  changeAccountEmail(currentPassword: string, email: string) { return this.request<{ message: string }>("/api/v1/admin/account/email", { method: "POST", body: JSON.stringify({ current_password: currentPassword, email }) }, true); }
  deleteAccount(currentPassword: string, familyName: string) { return this.request<void>("/api/v1/admin/account", { method: "DELETE", body: JSON.stringify({ current_password: currentPassword, family_name: familyName }) }, true); }
  private async device(id: string): Promise<Device> {
    const detail = await this.request<DeviceDetailResponse>(`/api/v1/admin/devices/${id}`);
    return {
      id: detail.device.id, name: detail.device.name, avatar_key: normalizeAvatarKey(detail.device.avatar_key, detail.device.id),
      online: detail.status.online,
      graphical_session_active: detail.status.graphical_session_active,
      monitoring_paused: detail.control.monitoring_paused, manual_block: detail.control.manual_block,
      actual_state: detail.status.actual_state ?? (detail.status.online ? "unblocked" : "offline"),
      control_status: detail.status.control_status ?? (!detail.status.online ? "offline" : detail.control.monitoring_paused ? "paused" : detail.control.manual_block ? "blocked" : "active"),
      counting: detail.status.counting, used_seconds: detail.status.used_seconds,
      remaining_seconds: detail.status.remaining_seconds, bonus_seconds: detail.status.bonus_seconds,
      today_quota_seconds: detail.status.today_quota_seconds, warning_minutes: detail.policy.warning_minutes,
      last_seen_at: detail.device.last_seen_at, weekly_quota_seconds: normalizedWeeklyQuota(detail.policy?.weekly_quota_seconds),
      routines: normalizedRoutines(detail.policy?.routines), password_set: Boolean(detail.policy?.password_set),
    };
  }
  async devices() {
    const list = await this.request<{ devices: DeviceResponse[] }>("/api/v1/admin/devices");
    return Promise.all(list.devices.map((summary) => this.device(summary.id)));
  }
  loadDevice(id: string) { return this.device(id); }
  createDevice(name: string, avatarKey: AvatarKey) { return this.request<DeviceResponse>("/api/v1/admin/devices", { method: "POST", body: JSON.stringify({ name, avatar_key: avatarKey }) }, true); }
  deleteDevice(id: string, currentPassword: string) { return this.request<void>(`/api/v1/admin/devices/${id}`, { method: "DELETE", body: JSON.stringify({ current_password: currentPassword }) }, true); }
  rename(id: string, name: string, avatarKey: AvatarKey) { return this.request(`/api/v1/admin/devices/${id}`, { method: "PATCH", body: JSON.stringify({ name, avatar_key: avatarKey }) }, true); }
  command(id: string, command: string) { return this.request<{ message: string; operation_id: string }>(`/api/v1/admin/devices/${id}/commands`, { method: "POST", body: JSON.stringify({ command }) }, true); }
  bonus(id: string, minutes: number) { return this.request<{ message: string; operation_id: string }>(`/api/v1/admin/devices/${id}/bonus`, { method: "POST", body: JSON.stringify({ minutes }) }, true); }
  activities(id: string) { return this.request<DeviceActivitiesResponse>(`/api/v1/admin/devices/${id}/activities?limit=100`); }
  deleteCompletedActivities(id: string) { return this.request<{ deleted: number }>(`/api/v1/admin/devices/${id}/activities/completed`, { method: "DELETE" }, true); }
  openStream(id: string): EventSource {
    return new EventSource(`${this.base}/api/v1/admin/devices/${encodeURIComponent(id)}/stream`, { withCredentials: true });
  }
  policy(id: string, weekly: number[], warning: number) { return this.request(`/api/v1/admin/devices/${id}/policy`, { method: "PUT", body: JSON.stringify({ weekly_quota_seconds: weekly, warning_minutes: warning }) }, true); }
  updatePassword(id: string, password: string, confirmation: string) { return this.request(`/api/v1/admin/devices/${id}/password`, { method: "PUT", body: JSON.stringify({ password, password_confirmation: confirmation }) }, true); }
  issueToken(id: string, currentPassword: string) { return this.request<{ device_id: string; device_token: string }>(`/api/v1/admin/devices/${id}/token`, { method: "POST", body: JSON.stringify({ current_password: currentPassword }) }, true); }
  revokeToken(id: string, currentPassword: string) { return this.request<void>(`/api/v1/admin/devices/${id}/token`, { method: "DELETE", body: JSON.stringify({ current_password: currentPassword }) }, true); }
  routine(id: string, routine: Omit<Routine, "id">, routineId?: string) { return this.request<{ id: string }>(`/api/v1/admin/devices/${id}/routines${routineId ? `/${routineId}` : ""}`, { method: routineId ? "PUT" : "POST", body: JSON.stringify(routine) }, true); }
  deleteRoutine(id: string, routineId: string) { return this.request(`/api/v1/admin/devices/${id}/routines/${routineId}`, { method: "DELETE" }, true); }
  communication(id: string, after = 0) { return this.request<CommunicationResponse>(`/api/v1/admin/devices/${id}/communication?limit=200${after ? `&after=${after}` : ""}`); }
  deleteCommunication(id: string) { return this.request<{ deleted: number }>(`/api/v1/admin/devices/${id}/communication`, { method: "DELETE" }, true); }
}

export const api = new API();
