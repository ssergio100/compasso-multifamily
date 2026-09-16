import { ArrowLeft, ArrowRight, Check, RefreshCw, X } from "lucide-react";
import { Component, useEffect, useRef, useState, type CSSProperties, type ErrorInfo, type KeyboardEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";

export function Brand() {
  return <span className="brand" aria-label="Compasso"><svg aria-hidden="true" viewBox="0 0 52 32"><path d="M5 23V18M15 23V11M26 23V4M37 23V11M47 23V18" /><path className="base" d="M4 28H48" /></svg><strong>Compasso</strong></span>;
}

export class AppErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null };
  static getDerivedStateFromError(error: Error) { return { error }; }
  componentDidCatch(error: Error, info: ErrorInfo) { console.error("Falha recuperável na interface ADM:", error, info); }
  render() {
    if (this.state.error) {
      return <div className="center-state" role="alert"><Brand /><h1>Não foi possível mostrar esta tela</h1><p>A ação anterior pode ter sido concluída. Recarregue para buscar o estado salvo no servidor.</p><button className="primary-button" onClick={() => window.location.reload()}><RefreshCw size={18} />Recarregar</button></div>;
    }
    return this.props.children;
  }
}

export function Modal({ title, description, children, onClose }: { title: string; description?: string; children: ReactNode; onClose: () => void }) {
  return <div className="modal-backdrop" onMouseDown={(event) => event.target === event.currentTarget && onClose()}><section aria-label={title} className="modal" role="dialog" aria-modal="true"><header><div><h2>{title}</h2>{description && <p>{description}</p>}</div><button aria-label="Fechar" onClick={onClose}><X size={20} /></button></header>{children}</section></div>;
}

export function Toast({ children }: { children: ReactNode }) { return <div className="toast" role="status"><Check size={17} />{children}</div>; }

function padTime(value: number) { return String(value).padStart(2, "0"); }
const timeStepMinutes = 15;
const lastTimeMinutes = 1440 - timeStepMinutes;
function normalizedTime(value: string) { const [rawHour, rawMinute] = value.split(":").map(Number); const rounded = Math.round(rawMinute / timeStepMinutes) * timeStepMinutes; return `${padTime((rawHour + Math.floor(rounded / 60)) % 24)}:${padTime(rounded % 60)}`; }
function timeMinutes(value: string) { const [hour, minute] = value.split(":").map(Number); return hour * 60 + minute; }
function minutesTime(value: number) { const minutes = (value + 1440) % 1440; return `${padTime(Math.floor(minutes / 60))}:${padTime(minutes % 60)}`; }

const durationStepSeconds = 15 * 60;
const daySeconds = 24 * 60 * 60;
function durationLabel(seconds: number) { const totalMinutes = Math.round(seconds / 60); const hours = Math.floor(totalMinutes / 60); const minutes = totalMinutes % 60; if (!hours) return `${minutes}min`; if (!minutes) return `${hours}h`; return `${hours}h ${padTime(minutes)}min`; }

export function DurationWheel({ value, label, caption, onChange, onCommit }: { value: number; label: string; caption?: string; onChange: (value: number) => void; onCommit?: (value: number) => void }) {
  const dragging = useRef(false);
  const normalized = Math.min(daySeconds, Math.max(0, Math.round(value / durationStepSeconds) * durationStepSeconds));
  const currentValue = useRef(normalized);
  currentValue.current = normalized;
  const angle = normalized / daySeconds * 360;
  const radians = (angle - 90) * Math.PI / 180;
  const knobStyle: CSSProperties = { left: `calc(50% + ${Math.cos(radians) * 43.7}%)`, top: `calc(50% + ${Math.sin(radians) * 43.7}%)` };
  const ringStyle: CSSProperties = { background: `conic-gradient(from 0deg, var(--ink) 0deg ${angle}deg, var(--line) ${angle}deg 360deg)` };
  const updateFromPointer = (event: ReactPointerEvent<HTMLDivElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect();
    const x = event.clientX - bounds.left - bounds.width / 2;
    const y = event.clientY - bounds.top - bounds.height / 2;
    if (Math.hypot(x, y) < bounds.width * .22) return;
    const pointerAngle = (Math.atan2(y, x) * 180 / Math.PI + 90 + 360) % 360;
    const nextValue = Math.round(pointerAngle / 360 * daySeconds / durationStepSeconds) * durationStepSeconds;
    currentValue.current = nextValue;
    onChange(nextValue);
  };
  const adjustWithKeyboard = (event: KeyboardEvent<HTMLDivElement>) => {
    if (!["ArrowLeft", "ArrowDown", "ArrowRight", "ArrowUp", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    const nextValue = event.key === "Home" ? 0 : event.key === "End" ? daySeconds : Math.min(daySeconds, Math.max(0, normalized + (["ArrowRight", "ArrowUp"].includes(event.key) ? durationStepSeconds : -durationStepSeconds)));
    currentValue.current = nextValue;
    onChange(nextValue);
  };
  const finishAdjustment = () => onCommit?.(currentValue.current);

  return <div className="duration-wheel"><div aria-label={`${label}${caption ? ` · ${caption}` : ""}: ${durationLabel(normalized)}`} aria-valuemax={daySeconds} aria-valuemin={0} aria-valuenow={normalized} aria-valuetext={durationLabel(normalized)} className="time-ring duration-ring" onKeyDown={adjustWithKeyboard} onKeyUp={(event) => { if (["ArrowLeft", "ArrowDown", "ArrowRight", "ArrowUp", "Home", "End"].includes(event.key)) finishAdjustment(); }} onPointerDown={(event) => { dragging.current = true; event.currentTarget.setPointerCapture(event.pointerId); updateFromPointer(event); }} onPointerMove={(event) => { if (dragging.current) updateFromPointer(event); }} onPointerCancel={() => { if (dragging.current) { dragging.current = false; finishAdjustment(); } }} onPointerUp={(event) => { if (!dragging.current) return; dragging.current = false; finishAdjustment(); event.currentTarget.releasePointerCapture(event.pointerId); }} role="slider" style={ringStyle} tabIndex={0}><div><span>{label.toUpperCase()}</span><strong>{durationLabel(normalized)}</strong>{caption && <small>{caption.toUpperCase()}</small>}</div><i aria-hidden="true" style={knobStyle} /></div></div>;
}

export function TimeRangePicker({ start, end, onChange }: { start: string; end: string; onChange: (start: string, end: string) => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const dragging = useRef(false);
  const [open, setOpen] = useState(false);
  const [phase, setPhase] = useState<"start" | "end">("start");
  const [draftStart, setDraftStart] = useState(start);
  const [draftEnd, setDraftEnd] = useState(end);
  useEffect(() => {
    if (open) {
      dialog.current?.showModal();
    } else if (dialog.current?.open) dialog.current.close();
  }, [open]);

  const draft = phase === "start" ? draftStart : draftEnd;
  const setDraft = phase === "start" ? setDraftStart : setDraftEnd;
  const openPicker = (nextPhase: "start" | "end") => {
    setDraftStart(normalizedTime(start));
    setDraftEnd(normalizedTime(end));
    setPhase(nextPhase);
    setOpen(true);
  };
  const updateFromPointer = (event: ReactPointerEvent<HTMLDivElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect();
    const x = event.clientX - bounds.left - bounds.width / 2;
    const y = event.clientY - bounds.top - bounds.height / 2;
    if (Math.hypot(x, y) < bounds.width * .22) return;
    const angle = (Math.atan2(y, x) * 180 / Math.PI + 90 + 360) % 360;
    setDraft(minutesTime(Math.round(angle / 360 * 1440 / timeStepMinutes) * timeStepMinutes));
  };

  const minutes = timeMinutes(draft);
  const angle = minutes / 1440 * 360;
  const radians = (angle - 90) * Math.PI / 180;
  const knobStyle: CSSProperties = { left: `calc(50% + ${Math.cos(radians) * 43.7}%)`, top: `calc(50% + ${Math.sin(radians) * 43.7}%)` };
  const ringStyle: CSSProperties = { background: `conic-gradient(from 0deg, var(--ink) 0deg ${angle}deg, var(--line) ${angle}deg 360deg)` };
  const adjustWithKeyboard = (event: KeyboardEvent<HTMLDivElement>) => { if (!["ArrowLeft", "ArrowDown", "ArrowRight", "ArrowUp", "Home", "End"].includes(event.key)) return; event.preventDefault(); if (event.key === "Home") setDraft("00:00"); else if (event.key === "End") setDraft(minutesTime(lastTimeMinutes)); else setDraft(minutesTime(minutes + (["ArrowRight", "ArrowUp"].includes(event.key) ? timeStepMinutes : -timeStepMinutes))); };
  const finish = () => { onChange(draftStart, draftEnd); setOpen(false); };

  return <div className="time-range-field"><span>Horário</span><div className="time-range-control"><button aria-label={`Começa: ${start}`} onClick={() => openPicker("start")} type="button"><small>Começa</small><strong>{start}</strong></button><ArrowRight aria-hidden="true" size={18} /><button aria-label={`Termina: ${end}`} onClick={() => openPicker("end")} type="button"><small>Termina</small><strong>{end}</strong></button></div><dialog className="time-dialog" ref={dialog} onCancel={(event) => { event.preventDefault(); setOpen(false); }}><div className="time-sheet"><header><div><strong>Horário da rotina</strong><span>{phase === "start" ? "1 de 2 · defina o início" : `2 de 2 · início definido às ${draftStart}`}</span></div><button aria-label="Fechar" onClick={() => setOpen(false)} type="button"><X size={20} /></button></header><div aria-label={`${phase === "start" ? "Começa" : "Termina"}: ${draft}`} aria-valuemax={lastTimeMinutes} aria-valuemin={0} aria-valuenow={minutes} aria-valuetext={draft} className="time-ring" onKeyDown={adjustWithKeyboard} onPointerDown={(event) => { dragging.current = true; event.currentTarget.setPointerCapture(event.pointerId); updateFromPointer(event); }} onPointerMove={(event) => { if (dragging.current) updateFromPointer(event); }} onPointerUp={(event) => { dragging.current = false; event.currentTarget.releasePointerCapture(event.pointerId); }} role="slider" style={ringStyle} tabIndex={0}><div><span>{phase === "start" ? "COMEÇA" : "TERMINA"}</span><strong>{draft}</strong></div><i aria-hidden="true" style={knobStyle} /></div><p className="ring-hint">Toque, arraste ou use as setas para ajustar de 15 em 15 minutos.</p>{phase === "start" ? <footer><button onClick={() => setOpen(false)} type="button">Cancelar</button><button className="primary" onClick={() => setPhase("end")} type="button">Definir término <ArrowRight size={17} /></button></footer> : <footer><button onClick={() => setPhase("start")} type="button"><ArrowLeft size={17} /> Editar início</button><button className="primary" onClick={finish} type="button"><Check size={17} /> Usar {draftStart}–{draftEnd}</button></footer>}</div></dialog></div>;
}
