import { AlertTriangle, Minus, Plus } from "lucide-react";
import { useState } from "react";
import { Modal } from "../../components";
import { formatDuration } from "../common/format";

const MIN_BONUS = 5;
const MAX_BONUS = 180;
const BONUS_STEP = 5;

export function BonusModal({ onClose, onSubmit }: { onClose: () => void; onSubmit: (minutes: number) => Promise<void> }) {
  const [value, setValue] = useState(30);
  const [custom, setCustom] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const adjust = (delta: number) => () => { setValue((current) => Math.min(MAX_BONUS, Math.max(MIN_BONUS, current + delta))); setCustom(true); setError(""); };
  const confirm = async () => { setBusy(true); setError(""); try { await onSubmit(value); } catch (submitError) { setError(submitError instanceof Error ? submitError.message : "Não foi possível adicionar tempo."); } finally { setBusy(false); } };
  return <Modal title="Mais tempo" description="O tempo extra vale somente para hoje." onClose={onClose}><div className="preset-grid">{[15, 30, 45, 60].map((item) => <button className={value === item && !custom ? "active" : ""} key={item} onClick={() => { setValue(item); setCustom(false); setError(""); }}><strong>{item}</strong><span>min</span></button>)}</div><div className="custom-stepper"><span className="custom-divider">Ou escolha outro valor</span><div className="stepper-control"><button disabled={busy || value <= MIN_BONUS} onClick={adjust(-BONUS_STEP)} type="button"><Minus aria-hidden="true" size={18} /><small>5 min</small></button><button className="stepper-submit" disabled={busy} onClick={() => void confirm()} type="button"><strong>{formatDuration(value * 60)}</strong><small>adicionar</small></button><button disabled={busy || value >= MAX_BONUS} onClick={adjust(BONUS_STEP)} type="button"><Plus aria-hidden="true" size={18} /><small>5 min</small></button></div><small className="custom-hint">De {MIN_BONUS} min a {MAX_BONUS} min (3h).</small></div>{error && <div className="routine-conflict-alert" role="alert"><AlertTriangle aria-hidden="true" size={20} /><div><strong>Tempo não adicionado</strong><span>{error}</span></div></div>}<div className="modal-actions single"><button onClick={onClose}>Cancelar</button></div></Modal>;
}