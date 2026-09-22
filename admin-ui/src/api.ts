import type { AvatarKey, CommunicationResponse, Device, DeviceActivitiesResponse, DeviceDetailResponse, DeviceResponse, Routine, Session } from "./types";
import { inferRoutineIcon, isRoutineIconKey, normalizeAvatarKey } from "./visuals";

const productionAPIBase = import.meta.env.PROD
  ? `${window.location.protocol}//${window.location.hostname}:8181`
  : "";
const runtimeBase = (window.COMPASSO_CONFIG?.apiBaseUrl ?? window.COMPASSO_CONFIG?.apiBaseURL ?? "").trim();
const envBase = (import.meta.env.VITE_COMPASSO_API_BASE_URL ?? "").trim();
const visualPreview = import.meta.env.DEV && new URLSearchParams(window.location.search).get("preview") === "visuals";
export const remoteMode = !visualPreview && (import.meta.env.VITE_COMPASSO_REMOTE === "true" || Boolean(runtimeBase || envBase || productionAPIBase));

type APIErrorPayload = {
  error?: unknown;
  code?: unknown;
  correlation_id?: unknown;
};

const API_ERROR_MESSAGES: Record<string, string> = {
  invalid_credentials: "E-mail ou senha inválidos.",
  authentication_required: "Sua sessão expirou. Entre novamente.",
  invalid_csrf_token: "Sua sessão de segurança expirou. Atualize a página e tente novamente.",
  origin_not_allowed: "Este endereço da interface não está autorizado pela API.",
  login_rate_limited: "Muitas tentativas de entrada. Aguarde 15 minutos e tente novamente.",
  rate_limited: "Muitas solicitações em pouco tempo. Aguarde e tente novamente.",
  invalid_registration: "Revise os dados do cadastro e tente novamente.",
  registration_start_failed: "A API não conseguiu iniciar o cadastro.",
  confirmation_request_failed: "A API não conseguiu solicitar a confirmação por e-mail.",
  password_reset_request_failed: "A API não conseguiu solicitar a recuperação de senha.",
  confirmation_invalid_or_expired: "O link de confirmação é inválido, expirou ou já foi usado.",
  registrations_closed: "Novos cadastros estão temporariamente indisponíveis.",
  password_reset_invalid: "Revise os dados da nova senha.",
  password_reset_invalid_or_expired: "O link de recuperação é inválido, expirou ou já foi usado.",
  password_change_failed: "A API não conseguiu alterar a senha.",
  account_security_failed: "A API não conseguiu proteger os dados da conta.",
  password_security_failed: "A API não conseguiu proteger a nova senha.",
  invalid_password: "A senha informada não atende aos requisitos.",
  password_confirmation_mismatch: "A senha e a confirmação precisam ser iguais.",
  current_password_invalid: "A senha atual está incorreta.",
  current_account_password_invalid: "A senha da conta está incorreta.",
  account_identity_invalid: "A senha atual ou o e-mail informado está incorreto.",
  family_name_mismatch: "O nome digitado não corresponde ao nome da família.",
  email_unavailable: "Este e-mail não está disponível.",
  email_change_failed: "A API não conseguiu alterar o e-mail.",
  email_delivery_unavailable: "O serviço de envio de e-mail está temporariamente indisponível.",
  account_delete_failed: "A API não conseguiu excluir a conta.",
  device_limit_reached: "Esta família atingiu o limite de computadores.",
  invalid_device: "Revise os dados do computador e tente novamente.",
  device_not_found: "O computador não foi encontrado.",
  device_resource_not_found: "O computador ou o item solicitado não foi encontrado.",
  devices_load_failed: "A API não conseguiu carregar os computadores.",
  device_load_failed: "A API não conseguiu carregar o computador.",
  device_authorization_failed: "A API não conseguiu confirmar o acesso ao computador.",
  device_password_update_failed: "A API não conseguiu atualizar a senha local.",
  invalid_bonus: "O tempo extra deve estar entre 1 minuto e 12 horas.",
  invalid_limit: "O limite informado não é válido.",
  invalid_cursor: "A posição solicitada no histórico não é válida.",
  audit_events_load_failed: "A API não conseguiu carregar o histórico.",
  communication_settings_load_failed: "A API não conseguiu carregar as configurações de diagnóstico.",
  routine_conflict: "Este horário está ocupado por outra rotina.",
  streaming_unavailable: "As atualizações em tempo real estão indisponíveis.",
  method_not_allowed: "A API não aceita esta operação neste endereço.",
  not_found: "O endereço solicitado não foi encontrado na API.",
  invalid_request: "A API não conseguiu validar os dados enviados.",
  request_forbidden: "A API recusou esta operação.",
  request_conflict: "A operação conflita com o estado atual dos dados.",
  service_unavailable: "A API está temporariamente indisponível.",
  internal_error: "A API encontrou um erro interno ao processar a operação.",
};

export class APIError extends Error {
  readonly code: string;
  readonly correlationID: string;
  readonly status: number;

  constructor(message: string, code: string, correlationID: string, status: number) {
    super(message);
    this.name = "APIError";
    this.code = code;
    this.correlationID = correlationID;
    this.status = status;
  }
}

function payloadText(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function withDiagnostics(message: string, code: string, correlationID: string): string {
  const details = [code ? `Código: ${code}` : "", correlationID ? `Referência: ${correlationID}` : ""].filter(Boolean);
  return details.length ? `${message.replace(/[.\s]+$/, "")}. ${details.join(" · ")}.` : message;
}

function responseError(response: Response, isJSON: boolean, payload: APIErrorPayload): APIError {
  const serverMessage = payloadText(payload.error);
  const responseCode = payloadText(payload.code) || response.headers.get("X-Compasso-Error-Code")?.trim() || "";
  const correlationID = payloadText(payload.correlation_id) || response.headers.get("X-Compasso-Correlation-ID")?.trim() || "";
  if (response.status === 405 && !isJSON) {
    const code = "api_endpoint_misconfigured";
    return new APIError(withDiagnostics(
      "A interface está apontando para o servidor de arquivos, não para a API. Revise runtime-config.js.",
      code,
      correlationID,
    ), code, correlationID, response.status);
  }
  if (!isJSON) {
    const code = responseCode || (response.status === 403
      ? "api_access_forbidden"
      : response.status === 404
        ? "api_endpoint_not_found"
        : response.status >= 500
          ? "api_gateway_error"
          : "api_http_error");
    const message = response.status === 403
      ? "O acesso à API foi recusado antes que o aplicativo pudesse responder."
      : response.status === 404
        ? "O endereço configurado para a API não foi encontrado."
        : response.status >= 500
          ? "A infraestrutura não conseguiu obter uma resposta válida da API."
          : `A API respondeu com o estado HTTP ${response.status}.`;
    return new APIError(withDiagnostics(message, code, correlationID), code, correlationID, response.status);
  }
  const code = responseCode || `http_${response.status}`;
  const message = code === "routine_conflict" && serverMessage
    ? serverMessage
    : API_ERROR_MESSAGES[code] ?? "A API não conseguiu concluir a operação.";
  return new APIError(withDiagnostics(message, code, correlationID), code, correlationID, response.status);
}

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
    let response: Response;
    try {
      response = await fetch(`${this.base}${path}`, { ...init, headers, credentials: "include", cache: "no-store" });
    } catch {
      const code = "api_unreachable";
      throw new APIError(withDiagnostics(
        "Não foi possível conectar à API. Verifique a conexão e o endereço configurado.",
        code,
        "",
      ), code, "", 0);
    }
    if (response.status === 204) return undefined as T;
    const isJSON = response.headers.get("Content-Type")?.includes("application/json") ?? false;
    const payload = (isJSON ? await response.json().catch(() => ({})) : {}) as APIErrorPayload;
    if (!response.ok) {
      throw responseError(response, isJSON, payload);
    }
    return payload as T;
  }

  async session() { const value = await this.request<Session>("/api/v1/admin/session"); this.csrf = value.csrf_token; return value; }
  async login(login: string, password: string) {
    await this.session();
    const value = await this.request<Session>("/api/v1/admin/session", { method: "POST", body: JSON.stringify({ login, password, csrf_token: this.csrf }) });
    this.csrf = value.csrf_token; return value;
  }
  async logout() { await this.request<void>("/api/v1/admin/session", { method: "DELETE" }, true); }
  async register(familyName: string, email: string, password: string, confirmation: string) {
    await this.session();
    return this.request<{ message: string }>("/api/v1/account/register", { method: "POST", body: JSON.stringify({ family_name: familyName, email, password, password_confirmation: confirmation, csrf_token: this.csrf }) });
  }
  async resendConfirmation(email: string) {
    await this.session();
    return this.request<{ message: string }>("/api/v1/account/resend-confirmation", { method: "POST", body: JSON.stringify({ email, csrf_token: this.csrf }) });
  }
  async requestPasswordReset(email: string) {
    await this.session();
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
	  session_lock_supported: detail.status.session_lock_supported,
	  unlock_authentication_required: detail.status.unlock_authentication_required,
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
