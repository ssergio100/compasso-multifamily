import { CalendarDays, Clock3, LockKeyhole, Monitor, MonitorCheck, Pause, Play, Plus, RefreshCw, ShieldCheck, UnlockKeyhole, UserRoundCheck } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import type { Device } from "../../types";
import { formatDuration, lastSeen } from "../common/format";
import { StatusRow } from "../common/StatusRow";
import { deviceIsBlockedForAction, deviceVisualState } from "../devices/devicePresentation";
import { clock, nextRoutineLabel, routineSegmentsForDay } from "../routines/routineSchedule";

function DetailSection({ title, Icon, children }: { title: string; Icon: typeof Clock3; children: ReactNode }) {
  return <section className="detail-section"><h3><Icon size={21} />{title}</h3>{children}</section>;
}

// A versão por extenso permanece no DOM (time-hero-number), hoje restrita ao
// modo de mesa por CSS. Um futuro ajuste pessoal pode alternar entre as duas
// visualizações apenas invertendo o `display` destes elementos.
function TimeBalanceRing({ usedSeconds, limitSeconds, remaining, stateNote }: { usedSeconds: number; limitSeconds: number; remaining: number; stateNote: string }) {
  const consumedDegrees = limitSeconds > 0 ? Math.min(360, (usedSeconds / limitSeconds) * 360) : 360;
  const ringStyle = { background: `conic-gradient(from 0deg, var(--ink) 0deg ${consumedDegrees}deg, var(--coral) ${consumedDegrees}deg 360deg)` };
  const note = stateNote === "restantes hoje" ? null : stateNote;
  return <div className="time-balance">{note && <small className="balance-note">{note}</small>}<div className="time-ring balance-ring" style={ringStyle}><div><span>TEMPO RESTANTE</span><strong>{formatDuration(remaining)}</strong></div></div></div>;
}

export function NowPage({ device, onBonus, onPause, onBlock }: { device: Device; onBonus: () => void; onPause: () => void; onBlock: () => void }) {
  const remaining = device.remaining_seconds;
  const [now, setNow] = useState(() => new Date());
  useEffect(() => { const timer = window.setInterval(() => setNow(new Date()), 30000); return () => window.clearInterval(timer); }, []);
  const minutes = now.getHours() * 60 + now.getMinutes();
  const daySegments = [-1, 0, 1].map((offset) => ({
    offset,
    when: ["ontem", "hoje", "amanhã"][offset + 1],
    segments: routineSegmentsForDay(device.routines, (now.getDay() + offset + 7) % 7),
  }));
  const hasRoutineSegments = daySegments.some(({ segments }) => segments.length > 0);
  const visualState = deviceVisualState(device);
  const blockedForAction = deviceIsBlockedForAction(device);
  const blockTransition = device.control_status === "block_requested" || device.control_status === "unblock_requested";
  const blockButtonLabel = device.control_status === "block_requested" ? "Bloqueando…"
    : device.control_status === "unblock_requested" ? "Desbloqueando…"
      : blockedForAction ? "Desbloquear"
        : "Bloquear";
  const pauseTransition = device.control_status === "pause_requested" || device.control_status === "resume_requested";
  const pauseButtonLabel = device.control_status === "pause_requested" ? "Pausando…"
    : device.control_status === "resume_requested" ? "Retomando…"
      : device.monitoring_paused ? "Retomar"
        : "Pausar";
  const monitorState = device.control_status === "pause_requested" ? "Pausando…"
    : device.control_status === "resume_requested" ? "Retomando…"
      : device.control_status === "paused" ? "Pausado"
        : !device.graphical_session_active ? "Aguardando sessão"
          : "Ativo";
  const accessState = device.control_status === "block_requested" ? "Bloqueando…"
    : device.control_status === "unblock_requested" ? "Desbloqueando…"
      : device.actual_state === "blocked" ? "Bloqueado"
        : !device.graphical_session_active ? "Sem sessão"
          : "Liberado";
  const timeCaption = !device.online ? "saldo calculado com os últimos dados" : visualState === "blocked" || visualState === "paused" ? "tempo restante preservado" : "restantes hoje";

  return <section className={`rhythm-board state-${visualState}`}><h2 className="section-name">Agora</h2><div className="time-hero"><strong className="time-hero-number">{formatDuration(remaining)}</strong><span>{timeCaption}</span><TimeBalanceRing usedSeconds={device.used_seconds} limitSeconds={device.today_quota_seconds + device.bonus_seconds} remaining={remaining} stateNote={timeCaption} /></div><div className="day-track"><div className="day-window"><div className="day-strip" style={{ transform: `translateX(${(-(minutes / 1440) * 100 - 50) / 3}%)` }}>{daySegments.map(({ offset, when, segments }) => <div className={offset === 0 ? "day-cell" : "day-cell adjacent"} key={offset}><div className="track-labels"><span>00:00</span><span>06:00</span><b>12:00</b><span>18:00</span><span /></div>{segments.length > 0 && <div aria-label={`Rotinas de bloqueio de ${when}`} className="routine-bands">{segments.map((segment) => { const start = clock(segment.start); const end = segment.end === 86400 ? "24:00" : clock(segment.end); const description = `${segment.name}: bloqueio das ${start} às ${end}`; return <span aria-label={`${description} de ${when}`} key={segment.key} role="img" style={{ left: `${(segment.start / 86400) * 100}%`, width: `${((segment.end - segment.start) / 86400) * 100}%` }} title={description} />; })}</div>}<div className="ticks" /></div>)}</div></div><i aria-hidden="true" /></div><div aria-hidden={!hasRoutineSegments} className={`track-legend ${hasRoutineSegments ? "" : "placeholder"}`}><span aria-hidden="true" /><LockKeyhole size={13} />Rotinas de bloqueio</div><dl className="time-facts"><div><dt>{device.online ? "Usado" : "Uso registrado"}</dt><dd>{formatDuration(device.used_seconds)}</dd></div><div><dt>Limite de hoje</dt><dd>{formatDuration(device.today_quota_seconds + device.bonus_seconds)}</dd></div></dl><div className="quick-actions"><button className="primary" onClick={onBonus}><Plus size={20} />Mais tempo</button><button className={pauseTransition ? "state-action pending" : device.monitoring_paused ? "state-action paused" : ""} disabled={!device.online || pauseTransition} onClick={onPause} title={!device.online ? "Disponível quando o computador estiver conectado" : pauseTransition ? "Aguardando confirmação do computador" : undefined}>{pauseTransition ? <RefreshCw size={20} /> : device.monitoring_paused ? <Play size={20} /> : <Pause size={20} />}{pauseButtonLabel}</button><button className={blockTransition ? "state-action pending" : blockedForAction ? "state-action blocked" : ""} disabled={!device.online || blockTransition} onClick={onBlock} title={!device.online ? "Disponível quando o computador estiver conectado" : blockTransition ? "Aguardando confirmação do computador" : undefined}>{blockTransition ? <RefreshCw size={20} /> : blockedForAction ? <UnlockKeyhole size={20} /> : <LockKeyhole size={20} />}{blockButtonLabel}</button></div><div className="detail-grid"><DetailSection title="Resumo de hoje" Icon={CalendarDays}><StatusRow Icon={Clock3} label="Próxima rotina" value={nextRoutineLabel(device.routines, now)} />{device.online && <StatusRow Icon={RefreshCw} label="Última sincronização" value={lastSeen(device.last_seen_at)} />}</DetailSection>{device.online && <DetailSection title="Estado do computador" Icon={Monitor}><StatusRow Icon={ShieldCheck} label="Agente" value="Conectado" /><StatusRow Icon={UserRoundCheck} label="Sessão gráfica" value={device.graphical_session_active ? "Ativa" : "Sem sessão"} /><StatusRow Icon={MonitorCheck} label="Monitoramento" value={monitorState} /><StatusRow Icon={LockKeyhole} label="Acesso" value={accessState} /><StatusRow Icon={Clock3} label="Contagem de tempo" value={device.counting ? "Em andamento" : "Parada"} /></DetailSection>}</div></section>;
}
