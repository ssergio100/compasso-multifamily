import {
  AlertTriangle, Check, ChevronDown, ChevronRight, CircleDashed, Clock3,
  Laptop, Radio, Search, Server, Trash2, UserRoundCheck, Wrench,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { api, remoteMode } from "../api";
import { Modal } from "../components";
import type {
  ActivityStep, CommunicationLog, CommunicationParty, CommunicationResult,
  DeviceActivity, DeviceActivityStatus, LiveStreamState,
} from "../types";
import { mockCommunication } from "./mockCommunication";

type PageTab = "activity" | "technical";
type ActivityFilter = "all" | "pending" | "completed";
type PartyFilter = "all" | CommunicationParty;
type ResultFilter = "all" | CommunicationResult;

const partyNames: Record<CommunicationParty, string> = {
  agent: "Agente", api: "Servidor", interface: "Interface",
};
const operationNames: Record<string, string> = {
  heartbeat: "Contato periódico do computador",
  heartbeat_response: "Resposta do servidor ao computador",
  "GET device": "Atualização do computador",
  "GET status": "Consulta de estado",
  "GET commands": "Consulta automática da atividade",
  "POST bonus": "Pedido de tempo extra",
  "POST commands": "Pedido de controle",
  "PUT policy": "Alteração de limites",
  "PUT routines": "Alteração de rotina",
  "POST routines": "Criação de rotina",
  "DELETE routines": "Exclusão de rotina",
};
const commandTitles: Record<string, (device: string) => string> = {
  pause_monitoring: (device) => `Monitoramento pausado no ${device}`,
  resume_monitoring: (device) => `Monitoramento retomado no ${device}`,
  block_now: (device) => `${device} bloqueado pelo administrador`,
  clear_manual_block: (device) => `${device} desbloqueado pelo administrador`,
};

function mockStep(kind: string, actor: ActivityStep["actor"], at: string, occurrences = 1): ActivityStep {
  return { kind, actor, occurred_at: at, last_occurred_at: at, occurrences, details: {} };
}

function mockActivities(deviceId: string): DeviceActivity[] {
  const now = Date.now();
  const completed = new Date(now - 4_000).toISOString();
  const offered = new Date(now - 12_000).toISOString();
  const requested = new Date(now - 18_000).toISOString();
  const localCreated = new Date(now - 48_000).toISOString();
  const localObserved = new Date(now - 42_000).toISOString();
  const pending = new Date(now - 90_000).toISOString();
  return [{
    id: "activity-admin-completed", device_id: deviceId, kind: "add_bonus",
    origin: "admin", status: "completed", details: { minutes: "15" },
    occurred_at: requested, observed_at: requested, completed_at: completed,
    steps: [
      mockStep("requested", "admin", requested),
      mockStep("stored", "server", requested),
      mockStep("offered", "server", offered),
      mockStep("completed", "device", completed),
    ],
  }, {
    id: "activity-local-completed", device_id: deviceId, kind: "add_bonus",
    origin: "device", status: "completed", details: { minutes: "30" },
    occurred_at: localCreated, observed_at: localObserved, completed_at: localObserved,
    steps: [
      mockStep("local_created", "device", localCreated),
      mockStep("synchronized", "server", localObserved),
      mockStep("confirmed", "server", localObserved),
    ],
  }, {
    id: "activity-admin-waiting", device_id: deviceId, kind: "pause_monitoring",
    origin: "admin", status: "waiting_device", details: { command: "pause_monitoring" },
    occurred_at: pending, observed_at: pending,
    steps: [mockStep("requested", "admin", pending), mockStep("stored", "server", pending)],
  }];
}

function operationName(value: string) {
  return operationNames[value] ?? value.replaceAll("_", " ");
}
function localTime(value: string) {
  return new Date(value).toLocaleTimeString("pt-BR", {
    hour: "2-digit", minute: "2-digit", second: "2-digit",
  });
}
function localDateTime(value: string) {
  return new Date(value).toLocaleString("pt-BR", { dateStyle: "short", timeStyle: "medium" });
}
function duration(from: string, to?: string) {
  if (!to) return "";
  const total = Math.max(1, Math.round((new Date(to).getTime() - new Date(from).getTime()) / 1000));
  if (total < 60) return `${total} ${total === 1 ? "segundo" : "segundos"}`;
  const minutes = Math.round(total / 60);
  return `${minutes} ${minutes === 1 ? "minuto" : "minutos"}`;
}
function activityStep(activity: DeviceActivity, kind: string) {
  return activity.steps.find((step) => step.kind === kind);
}
function deliveryText(step?: ActivityStep) {
  if (!step) return "Nenhum envio ao computador ainda";
  return `${step.occurrences} ${step.occurrences === 1 ? "envio" : "envios"} ao computador`;
}
function activityTitle(activity: DeviceActivity, deviceName: string) {
  if (activity.kind === "add_bonus") {
    return `${activity.details.minutes ?? "—"} min adicionados ${activity.origin === "device" ? "no" : "ao"} ${deviceName}`;
  }
  if (activity.kind === "device_created") return `${deviceName} adicionado ao ADM`;
  if (activity.kind === "device_renamed") return `Computador renomeado para ${activity.details.name || deviceName}`;
  if (activity.kind === "quotas_updated") return `Limites e aviso atualizados no ${deviceName}`;
  if (activity.kind === "routine_saved") {
    const action = activity.details.action === "created" ? "criada" : activity.details.action === "updated" ? "atualizada" : "salva";
    return `Rotina ${activity.details.name ? `“${activity.details.name}” ` : ""}${action}`;
  }
  if (activity.kind === "routine_deleted") return `Rotina ${activity.details.name ? `“${activity.details.name}” ` : ""}excluída`;
  if (activity.kind === "local_password_changed") return `Senha local alterada no ${deviceName}`;
  if (activity.kind === "device_token_issued") return `Nova chave de acesso gerada para o ${deviceName}`;
  if (activity.kind === "device_token_revoked") return `Chave de acesso revogada no ${deviceName}`;
  return commandTitles[activity.kind]?.(deviceName) ?? `Alteração solicitada para ${deviceName}`;
}
function activityState(status: DeviceActivityStatus) {
  if (status === "completed") return { label: "Concluído", tone: "success" };
  if (status === "failed") return { label: "Não aplicado", tone: "error" };
  if (status === "attention") return { label: "Precisa de atenção", tone: "warning" };
  if (status === "offered") return { label: "Aguardando confirmação", tone: "pending" };
  return { label: "Aguardando computador", tone: "pending" };
}
function activitySummary(activity: DeviceActivity, deviceName: string) {
  if (activity.origin === "device") {
    return `O tempo foi adicionado diretamente no ${deviceName} e o servidor confirmou a sincronização.`;
  }
  if (activityStep(activity, "completed")?.actor === "server") {
    return "O administrador fez a alteração e o servidor concluiu e registrou o resultado.";
  }
  if (activity.status === "completed") {
    return `${deviceName} aplicou a alteração e confirmou ao servidor em ${duration(activity.occurred_at, activity.completed_at)}.`;
  }
  if (activity.status === "offered") {
    return `O servidor enviou a alteração ao ${deviceName} e aguarda a confirmação.`;
  }
  if (activity.status === "failed") return `${deviceName} não conseguiu aplicar a alteração.`;
  return `O pedido está guardado no servidor e será enviado no próximo contato do ${deviceName}.`;
}

function ServerActivityTimeline({ activity }: { activity: DeviceActivity }) {
  const completed = activityStep(activity, "completed");
  return <ol className="operation-timeline">
    <li className="done"><UserRoundCheck size={18} /><span>
      <strong>Administrador fez a alteração</strong><small>{localDateTime(activity.occurred_at)}</small>
    </span></li>
    <li className={completed ? "done" : "pending"}><Server size={18} /><span>
      <strong>{completed ? "Servidor concluiu e registrou" : "Servidor está processando"}</strong>
      <small>{completed ? localDateTime(completed.occurred_at) : "Aguardando o resultado do servidor."}</small>
    </span></li>
  </ol>;
}
function mergeActivities(current: DeviceActivity[], incoming: DeviceActivity[]) {
  const merged = new Map(current.map((activity) => [activity.id, activity]));
  const progress = (activity: DeviceActivity) => {
    const status = { waiting_device: 0, offered: 1, attention: 2, failed: 3, completed: 4 }[activity.status];
    const latestStep = activity.steps.reduce((latest, step) => Math.max(latest, new Date(step.last_occurred_at).getTime()), 0);
    const occurrences = activity.steps.reduce((total, step) => total + step.occurrences, 0);
    return status * 1e16 + latestStep * 100 + occurrences;
  };
  incoming.forEach((activity) => {
    const existing = merged.get(activity.id);
    if (!existing || progress(activity) >= progress(existing)) merged.set(activity.id, activity);
  });
  return [...merged.values()]
    .sort((a, b) => new Date(b.occurred_at).getTime() - new Date(a.occurred_at).getTime())
    .slice(0, 200);
}
function mergeEvents(current: CommunicationLog[], incoming: CommunicationLog[]) {
  const known = new Set(current.map((event) => event.id));
  return [...incoming.filter((event) => !known.has(event.id)).reverse(), ...current].slice(0, 500);
}
function resultText(event: CommunicationLog) {
  if (event.result === "error") return event.http_status ? `${event.http_status} Falhou` : "Falhou";
  if (event.result === "warning") return event.http_status ? `${event.http_status} Atenção` : "Atenção";
  return event.http_status ? `${event.http_status} OK` : "Concluído";
}

function LocalActivityTimeline({ activity, deviceName }: { activity: DeviceActivity; deviceName: string }) {
  const created = activityStep(activity, "local_created");
  const synchronized = activityStep(activity, "synchronized");
  const confirmed = activityStep(activity, "confirmed");
  return <ol className="operation-timeline">
    <li className="done"><Laptop size={18} /><span>
      <strong>Tempo adicionado no {deviceName}</strong>
      <small>{localDateTime(created?.occurred_at ?? activity.occurred_at)}</small>
    </span></li>
    <li className={synchronized ? "done" : "pending"}><Server size={18} /><span>
      <strong>{synchronized ? "Agente enviou a informação ao servidor" : "Aguardando sincronização com o servidor"}</strong>
      <small>{synchronized ? localDateTime(synchronized.occurred_at) : "A informação será enviada no próximo contato."}</small>
    </span></li>
    <li className={confirmed ? "done" : "pending"}><Check size={18} /><span>
      <strong>{confirmed ? "Servidor recebeu e confirmou" : "Aguardando confirmação do servidor"}</strong>
      <small>{confirmed ? "A atividade já está registrada e o agente pode encerrar o envio." : "O registro ainda não foi confirmado."}</small>
    </span></li>
  </ol>;
}

function AdminActivityTimeline({ activity, deviceName }: { activity: DeviceActivity; deviceName: string }) {
  const offered = activityStep(activity, "offered");
  const completed = activityStep(activity, "completed");
  return <ol className="operation-timeline">
    <li className="done"><UserRoundCheck size={18} /><span>
      <strong>Administrador fez o pedido</strong><small>{localDateTime(activity.occurred_at)}</small>
    </span></li>
    <li className="done"><Server size={18} /><span>
      <strong>Servidor recebeu e guardou</strong><small>O pedido ficou salvo até o computador buscar atualizações.</small>
    </span></li>
    <li className={offered ? "done" : "pending"}><Server size={18} /><span>
      <strong>{offered ? `Servidor enviou ao ${deviceName}` : `Servidor aguarda o ${deviceName}`}</strong>
      <small>{offered ? `${deliveryText(offered)} · primeiro às ${localTime(offered.occurred_at)}` : "Nenhum contato do computador transportou este pedido ainda."}</small>
    </span></li>
    <li className={completed ? "done" : "pending"}><Laptop size={18} /><span>
      <strong>{completed ? `${deviceName} aplicou e confirmou` : `Aguardando confirmação do ${deviceName}`}</strong>
      <small>{completed ? localDateTime(completed.occurred_at) : "O sucesso só será mostrado depois da confirmação do computador."}</small>
    </span></li>
  </ol>;
}

export function CommunicationPage({
  activityUpdate, communicationUpdate, deviceId, deviceName, streamGeneration, streamState,
}: {
  activityUpdate: DeviceActivity | null;
  communicationUpdate: CommunicationLog | null;
  deviceId: string;
  deviceName: string;
  streamGeneration: number;
  streamState: LiveStreamState;
}) {
  const [tab, setTab] = useState<PageTab>("activity");
  const [activities, setActivities] = useState<DeviceActivity[]>(remoteMode ? [] : mockActivities(deviceId));
  const [events, setEvents] = useState<CommunicationLog[]>(remoteMode ? [] : mockCommunication);
  const [retentionDays, setRetentionDays] = useState(30);
  const [search, setSearch] = useState("");
  const [activityFilter, setActivityFilter] = useState<ActivityFilter>("all");
  const [party, setParty] = useState<PartyFilter>("all");
  const [result, setResult] = useState<ResultFilter>("all");
  const [expandedActivity, setExpandedActivity] = useState<string | null>(null);
  const [expandedLog, setExpandedLog] = useState<number | null>(null);
  const [error, setError] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmClearCompleted, setConfirmClearCompleted] = useState(false);
  const [busy, setBusy] = useState(false);
  const maxID = useRef(0);

  useEffect(() => {
    setExpandedActivity(null);
    setExpandedLog(null);
    setError("");
    if (!remoteMode) {
      setActivities(mockActivities(deviceId));
      setEvents(mockCommunication.map((event) => ({ ...event, device_id: deviceId })));
      return;
    }
	setActivities([]);
	setEvents([]);
	maxID.current = 0;
    let active = true;
    const refresh = async () => {
      try {
        const [activityResponse, communicationResponse] = await Promise.all([
          api.activities(deviceId), api.communication(deviceId),
        ]);
        if (!active) return;
        setActivities(activityResponse.activities);
        setEvents(communicationResponse.events);
        setRetentionDays(communicationResponse.retention_days);
        maxID.current = communicationResponse.events.length
          ? Math.max(...communicationResponse.events.map((event) => event.id)) : 0;
        setError("");
      } catch (loadError) {
        if (active) setError(loadError instanceof Error ? loadError.message : "Não foi possível atualizar a atividade.");
      }
    };
    void refresh();
    return () => { active = false; };
  }, [deviceId, streamGeneration]);

  useEffect(() => {
    if (activityUpdate?.device_id === deviceId) {
      setActivities((current) => mergeActivities(current, [activityUpdate]));
    }
  }, [activityUpdate, deviceId]);

  useEffect(() => {
    if (communicationUpdate?.device_id === deviceId) {
      setEvents((current) => mergeEvents(current, [communicationUpdate]));
      if (communicationUpdate.id > maxID.current) maxID.current = communicationUpdate.id;
    }
  }, [communicationUpdate, deviceId]);

  const visibleActivities = useMemo(() => {
    const term = search.trim().toLocaleLowerCase("pt-BR");
    return activities.filter((activity) => {
      if (activityFilter === "completed" && activity.status !== "completed") return false;
      if (activityFilter === "pending" && activity.status === "completed") return false;
      const searchable = `${activityTitle(activity, deviceName)} ${activitySummary(activity, deviceName)}`;
      return !term || searchable.toLocaleLowerCase("pt-BR").includes(term);
    });
  }, [activities, activityFilter, search, deviceName]);
  const completedActivities = useMemo(
    () => activities.filter((activity) => activity.status === "completed").length,
    [activities],
  );
  const visibleEvents = useMemo(() => {
    const term = search.trim().toLocaleLowerCase("pt-BR");
    return events.filter((event) => {
      if (party !== "all" && event.source !== party && event.target !== party) return false;
      if (result !== "all" && event.result !== result) return false;
      const searchable = [operationName(event.operation), event.summary, ...Object.values(event.details)].join(" ");
      return !term || searchable.toLocaleLowerCase("pt-BR").includes(term);
    });
  }, [events, party, result, search]);

  const deleteLogs = async () => {
    setBusy(true);
    try {
      if (remoteMode) await api.deleteCommunication(deviceId);
      setEvents([]);
      maxID.current = 0;
      setExpandedLog(null);
      setConfirmDelete(false);
    } catch (deleteError) {
      setError(deleteError instanceof Error ? deleteError.message : "Não foi possível excluir o diagnóstico.");
    } finally { setBusy(false); }
  };
  const clearCompletedActivities = async () => {
    setBusy(true);
    try {
      if (remoteMode) await api.deleteCompletedActivities(deviceId);
      setActivities((current) => current.filter((activity) => activity.status !== "completed"));
      setExpandedActivity(null);
      setConfirmClearCompleted(false);
    } catch (deleteError) {
      setError(deleteError instanceof Error ? deleteError.message : "Não foi possível limpar as ações concluídas.");
    } finally { setBusy(false); }
  };

  return <section className="communication-page">
    <header className="communication-heading"><div>
      <h2>Atividade</h2>
      <p>Veja o que foi feito no ADM ou no computador e acompanhe o resultado.</p>
      <div className={`live-indicator ${streamState}`}><span aria-hidden="true" />
        {streamState === "live" ? "Atualização ao vivo" : streamState === "connecting" ? "Conectando…" : "Atualização interrompida"}
      </div>
    </div></header>

    <div className="activity-tabs" role="tablist" aria-label="Tipo de histórico">
      <button aria-selected={tab === "activity"} className={tab === "activity" ? "active" : ""} onClick={() => setTab("activity")} role="tab"><Clock3 size={17} />Ações e resultados</button>
      <button aria-selected={tab === "technical"} className={tab === "technical" ? "active" : ""} onClick={() => setTab("technical")} role="tab"><Wrench size={17} />Diagnóstico técnico</button>
    </div>

    {error && <div className="communication-error" role="alert"><AlertTriangle size={19} /><span>
      <strong>Atualização incompleta</strong>{error}
    </span></div>}

    <div className={`communication-controls ${tab === "activity" ? "activity-controls" : ""}`}>
      <label className="log-search"><span className="sr-only">Buscar</span><Search aria-hidden="true" size={18} />
        <input placeholder={tab === "activity" ? "Buscar nas ações" : "Buscar no diagnóstico"} value={search} onChange={(event) => setSearch(event.target.value)} />
      </label>
      {tab === "activity" ? <>
        <label><span className="sr-only">Situação</span><select value={activityFilter} onChange={(event) => setActivityFilter(event.target.value as ActivityFilter)}>
          <option value="all">Todas as situações</option><option value="pending">Aguardando</option><option value="completed">Concluídas</option>
        </select></label>
        <button className="delete-logs" disabled={completedActivities === 0} onClick={() => setConfirmClearCompleted(true)}><Trash2 size={17} />Limpar concluídas</button>
      </> : <>
        <label><span className="sr-only">Participante</span><select value={party} onChange={(event) => setParty(event.target.value as PartyFilter)}>
          <option value="all">Todos os participantes</option><option value="agent">Computador</option><option value="api">Servidor</option><option value="interface">Interface</option>
        </select></label>
        <label><span className="sr-only">Resultado</span><select value={result} onChange={(event) => setResult(event.target.value as ResultFilter)}>
          <option value="all">Todos os resultados</option><option value="success">Concluídos</option><option value="warning">Com atenção</option><option value="error">Com falha</option>
        </select></label>
        <span className="retention-note">Retenção: {retentionDays} {retentionDays === 1 ? "dia" : "dias"}</span>
        <button className="delete-logs" onClick={() => setConfirmDelete(true)}><Trash2 size={17} />Excluir diagnóstico</button>
      </>}
    </div>

    {tab === "activity"
      ? <div className="operation-list">{visibleActivities.map((activity) => {
        const state = activityState(activity.status);
        const expanded = expandedActivity === activity.id;
        const offered = activityStep(activity, "offered");
        const completedByServer = activityStep(activity, "completed")?.actor === "server";
        return <article className={`operation-card ${state.tone}`} key={activity.id}>
          <button aria-expanded={expanded} className="operation-summary" onClick={() => setExpandedActivity(expanded ? null : activity.id)}>
            <span className={`operation-icon ${state.tone}`}>{activity.status === "completed" ? <Check size={19} /> : <CircleDashed size={19} />}</span>
            <span>
              <strong>{activityTitle(activity, deviceName)}</strong>
              <small>{activitySummary(activity, deviceName)}</small>
              <time dateTime={activity.occurred_at}>
                {activity.origin === "device" ? "Feito no computador" : "Pedido pelo ADM"} às {localTime(activity.occurred_at)}
                {activity.origin === "admin" && !completedByServer ? ` · ${deliveryText(offered)}` : ""}
              </time>
            </span>
            <b className={`operation-state ${state.tone}`}>{state.label}</b>
            {expanded ? <ChevronDown size={19} /> : <ChevronRight size={19} />}
          </button>
          {expanded && <div className="operation-detail">
            {activity.origin === "device"
              ? <LocalActivityTimeline activity={activity} deviceName={deviceName} />
              : completedByServer
                ? <ServerActivityTimeline activity={activity} />
                : <AdminActivityTimeline activity={activity} deviceName={deviceName} />}
            <details className="operation-technical"><summary>Ver detalhes técnicos</summary><dl>
              <div><dt>Atividade</dt><dd>{activity.id}</dd></div>
              <div><dt>Tipo</dt><dd>{activity.kind}</dd></div>
              <div><dt>Origem</dt><dd>{activity.origin}</dd></div>
            </dl></details>
          </div>}
        </article>;
      })}
      {!visibleActivities.length && <div className="log-empty"><Radio size={24} />
        <strong>{activities.length ? "Nenhuma ação corresponde aos filtros" : "Nenhuma ação registrada"}</strong>
        <span>Ações feitas no ADM e no computador aparecerão aqui como histórias completas.</span>
      </div>}</div>
      : <div className="log-table" role="table" aria-label={`Diagnóstico técnico de ${deviceName}`}>
        <div className="technical-intro"><Wrench size={18} /><span><strong>Informações para suporte</strong>Requisições, contatos periódicos e identificadores ficam separados das ações humanas.</span></div>
        <div className="log-table-header" role="row"><span>Hora</span><span>Evento técnico</span><span>Resultado</span><span /></div>
        {visibleEvents.map((event) => <div className={`log-entry ${event.result}`} key={event.id}>
          <button aria-expanded={expandedLog === event.id} className="log-row" onClick={() => setExpandedLog(expandedLog === event.id ? null : event.id)} role="row">
            <time dateTime={event.created_at}>{localTime(event.created_at)}</time>
            <span className="log-message"><strong>{operationName(event.operation)}</strong><small>{partyNames[event.source]} → {partyNames[event.target]}</small></span>
            <b className={`log-result ${event.result}`}>{resultText(event)}</b>
            {expandedLog === event.id ? <ChevronDown size={18} /> : <ChevronRight size={18} />}
          </button>
          {expandedLog === event.id && <div className="log-details"><dl>
            {Object.entries(event.details).map(([key, value]) => <div key={key}><dt>{key}</dt><dd>{value || "—"}</dd></div>)}
            <div><dt>Status HTTP</dt><dd>{event.http_status ?? "—"}</dd></div>
            <div><dt>Duração</dt><dd>{event.duration_ms ? `${event.duration_ms.toLocaleString("pt-BR")} ms` : "—"}</dd></div>
          </dl><p>Campos sensíveis foram omitidos.</p></div>}
        </div>)}
        {!visibleEvents.length && <div className="log-empty"><Radio size={24} /><strong>Nenhum registro técnico</strong><span>As ações humanas continuam preservadas na outra aba.</span></div>}
      </div>}

    <footer className="communication-footer">{tab === "activity" ? <>
      <span>{visibleActivities.length} {visibleActivities.length === 1 ? "ação mais recente exibida" : "ações mais recentes exibidas"}</span>
      <span>Ações concluídas saem do histórico após 30 dias. Bônus, configurações e pedidos em andamento são preservados.</span>
    </> : `${visibleEvents.length} registros técnicos exibidos`}</footer>

    {confirmClearCompleted && <Modal title="Limpar ações concluídas?" description={`As ações concluídas de “${deviceName}” deixarão de aparecer neste histórico.`} onClose={() => !busy && setConfirmClearCompleted(false)}>
      <div className="delete-log-warning"><AlertTriangle size={21} /><p>Isso não remove tempo, configurações nem pedidos pendentes. Remove somente a visualização das ações já concluídas.</p></div>
      <div className="modal-actions"><button disabled={busy} onClick={() => setConfirmClearCompleted(false)}>Cancelar</button><button className="danger-confirm" disabled={busy} onClick={() => void clearCompletedActivities()}><Trash2 size={17} />{busy ? "Limpando…" : "Limpar concluídas"}</button></div>
    </Modal>}
    {confirmDelete && <Modal title="Excluir diagnóstico técnico?" description={`Os registros técnicos de “${deviceName}” serão removidos.`} onClose={() => !busy && setConfirmDelete(false)}>
      <div className="delete-log-warning"><AlertTriangle size={21} /><p>As ações, os bônus e as configurações continuarão preservados.</p></div>
      <div className="modal-actions"><button disabled={busy} onClick={() => setConfirmDelete(false)}>Cancelar</button><button className="danger-confirm" disabled={busy} onClick={() => void deleteLogs()}><Trash2 size={17} />{busy ? "Excluindo…" : "Excluir diagnóstico"}</button></div>
    </Modal>}
  </section>;
}
