import { AlertTriangle, Check, Copy, KeyRound, LockKeyhole, MonitorCheck, RefreshCw, Save, Settings, ShieldCheck, ShieldOff, Trash2, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { AgentDownload } from "../../agentRelease";
import { Modal } from "../../components";
import type { AvatarKey, Device } from "../../types";
import { AvatarPicker } from "../../visuals";
import { lastSeen } from "../common/format";
import { StatusRow } from "../common/StatusRow";
import { avatarKeyFor } from "../devices/devicePresentation";

async function copyVisibleValue(value: string) {
  if (window.isSecureContext && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value);
      return;
    } catch { /* usa a cópia compatível abaixo */ }
  }
  const previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  const temporaryField = document.createElement("textarea");
  temporaryField.value = value;
  temporaryField.readOnly = true;
  temporaryField.setAttribute("aria-hidden", "true");
  temporaryField.style.cssText = "position:fixed;inset:0 auto auto -10000px;opacity:0;pointer-events:none";
  document.body.appendChild(temporaryField);
  temporaryField.select();
  temporaryField.setSelectionRange(0, value.length);
  let copied = false;
  try {
    copied = document.execCommand("copy");
  } finally {
    temporaryField.remove();
    previouslyFocused?.focus();
  }
  if (!copied) throw new Error("copy unavailable");
}

export function AdministrationPage({ device, onSave, onPassword, onIssueToken, onRevokeToken, onDelete }: {
  device: Device;
  onSave: (name: string, warning: number, avatarKey: AvatarKey) => Promise<void>;
  onPassword: (password: string, confirmation: string) => Promise<void>;
  onIssueToken: (currentPassword: string) => Promise<string>;
  onRevokeToken: (currentPassword: string) => Promise<void>;
  onDelete: (currentPassword: string) => Promise<void>;
}) {
  const [name, setName] = useState(device.name);
  const [warning, setWarning] = useState(device.warning_minutes);
  const [avatarKey, setAvatarKey] = useState<AvatarKey>(avatarKeyFor(device));
  const [busy, setBusy] = useState(false);
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [passwordBusy, setPasswordBusy] = useState(false);
  const [passwordTouched, setPasswordTouched] = useState(false);
  const [token, setToken] = useState("");
  const [tokenBusy, setTokenBusy] = useState(false);
  const [credentialAction, setCredentialAction] = useState<"issue" | "revoke" | null>(null);
  const [credentialPassword, setCredentialPassword] = useState("");
  const [credentialError, setCredentialError] = useState("");
  const [copyFeedback, setCopyFeedback] = useState<{ kind: "id" | "token"; status: "success" | "error" } | null>(null);
  const copyFeedbackTimer = useRef<number | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const [deletePassword, setDeletePassword] = useState("");
  const passwordError = passwordTouched && !password ? "Informe uma senha." : passwordTouched && password !== confirmation ? "As senhas não coincidem." : "";

  useEffect(() => { setName(device.name); setWarning(device.warning_minutes); setAvatarKey(avatarKeyFor(device)); }, [device.id]);
  useEffect(() => () => { if (copyFeedbackTimer.current !== null) window.clearTimeout(copyFeedbackTimer.current); }, []);

  const copyValue = async (value: string, kind: "id" | "token") => {
    if (copyFeedbackTimer.current !== null) window.clearTimeout(copyFeedbackTimer.current);
    try {
      await copyVisibleValue(value);
      setCopyFeedback({ kind, status: "success" });
      try { if ("vibrate" in navigator) navigator.vibrate(24); } catch { /* confirmação visual permanece disponível */ }
    } catch {
      setCopyFeedback({ kind, status: "error" });
    }
    copyFeedbackTimer.current = window.setTimeout(() => setCopyFeedback(null), 2200);
  };
  const copyButton = (value: string, kind: "id" | "token", label: string) => {
    const status = copyFeedback?.kind === kind ? copyFeedback.status : "idle";
    const text = status === "success" ? "Copiado!" : status === "error" ? "Não copiou" : "Copiar";
    return <button aria-label={status === "success" ? `${label} copiado` : status === "error" ? `Não foi possível copiar ${label.toLowerCase()}` : `Copiar ${label.toLowerCase()}`} className={`copy-button ${status}`} onClick={() => void copyValue(value, kind)} type="button">{status === "success" ? <Check aria-hidden="true" size={17} /> : status === "error" ? <X aria-hidden="true" size={17} /> : <Copy aria-hidden="true" size={17} />}<span aria-live="polite">{text}</span></button>;
  };

  return <section className="editor-page"><header><div><h2>Administração</h2><p>Identidade, acesso e segurança deste computador.</p></div></header><div className="administration-sections">
    <section className="admin-section identity-section"><div className="admin-section-heading"><Settings size={21} /><div><h3>Identidade</h3><p>Nome, avatar e aviso de bloqueio.</p></div></div><form className="admin-form" onSubmit={async (event) => { event.preventDefault(); setBusy(true); try { await onSave(name.trim(), warning, avatarKey); } finally { setBusy(false); } }}><label>Nome do computador<input value={name} onChange={(event) => setName(event.target.value)} /></label><AvatarPicker value={avatarKey} onChange={setAvatarKey} /><label>Aviso antes do fim<select value={warning} onChange={(event) => setWarning(Number(event.target.value))}>{[5, 10, 15, 30].map((item) => <option key={item} value={item}>{item} minutos</option>)}</select></label><button className="primary-button" disabled={!name.trim() || busy}><Save size={18} />{busy ? "Salvando…" : "Salvar informações"}</button></form></section>
    <section className="admin-section"><div className="admin-section-heading"><LockKeyhole size={21} /><div><h3>{device.password_set ? "Alterar senha local" : "Configurar senha local"}</h3><p>Autoriza tempo adicional diretamente no computador.</p></div></div><form className="admin-form" noValidate onSubmit={async (event) => { event.preventDefault(); setPasswordTouched(true); if (!password || password !== confirmation) return; setPasswordBusy(true); try { await onPassword(password, confirmation); setPassword(""); setConfirmation(""); setPasswordTouched(false); } finally { setPasswordBusy(false); } }}><label>Nova senha<input aria-invalid={Boolean(passwordError)} autoComplete="new-password" type="password" value={password} onBlur={() => setPasswordTouched(true)} onChange={(event) => setPassword(event.target.value)} /></label><label>Confirmar senha<input aria-invalid={Boolean(passwordError)} autoComplete="new-password" type="password" value={confirmation} onBlur={() => setPasswordTouched(true)} onChange={(event) => setConfirmation(event.target.value)} /></label>{passwordError && <p className="field-error"><X size={15} />{passwordError}</p>}<button className="primary-button" disabled={!password || password !== confirmation || passwordBusy}><Save size={18} />{passwordBusy ? "Salvando…" : "Salvar senha"}</button></form></section>
    <section className="admin-section pairing-section"><div className="admin-section-heading"><KeyRound size={21} /><div><h3>Liberar acesso do agente</h3><p>Baixe o instalador e use estes dados para conectar esta máquina.</p></div></div><AgentDownload showUnavailable /><div className="credential-field"><span>Identificador <code>device_id</code></span><div><code>{device.id}</code>{copyButton(device.id, "id", "Identificador")}</div></div>{token ? <div className="credential-field token-reveal"><span>Token — copie agora <code>device_token</code></span><p>Este token será exibido somente desta vez.</p><div><code>{token}</code>{copyButton(token, "token", "Token")}</div></div> : <p className="credential-note">O token atual não pode ser consultado. Gere um novo apenas quando for configurar o agente.</p>}<div className="credential-actions"><button className="primary-button" disabled={tokenBusy} onClick={() => { setCredentialPassword(""); setCredentialError(""); setCredentialAction("issue"); }}><RefreshCw size={18} />Gerar novo token</button><button className="danger-button" disabled={tokenBusy} onClick={() => { setCredentialPassword(""); setCredentialError(""); setCredentialAction("revoke"); }}><ShieldOff size={18} />Revogar token</button></div></section>
    <section className="technical admin-section"><div className="admin-section-heading"><MonitorCheck size={21} /><div><h3>Estado técnico</h3><p>Aplicação e sincronização.</p></div></div><StatusRow Icon={ShieldCheck} label="Senha local" value={device.password_set ? "Configurada" : "Não configurada"} /><StatusRow Icon={RefreshCw} label="Última sincronização" value={lastSeen(device.last_seen_at)} /></section>
    <section className="admin-section device-danger-zone"><div className="admin-section-heading"><Trash2 size={21} /><div><h3>Excluir computador</h3><p>Remove este computador e todas as suas configurações.</p></div></div><button className="danger-button" onClick={() => { setDeleteError(""); setDeletePassword(""); setDeleteOpen(true); }}><Trash2 size={18} />Excluir computador</button></section>
  </div>{credentialAction && <Modal title={credentialAction === "issue" ? "Gerar novo token?" : "Revogar token?"} description={credentialAction === "issue" ? "Se existir um token anterior, ele deixará de funcionar imediatamente." : "O agente perderá o acesso até que um novo token seja configurado."} onClose={() => !tokenBusy && setCredentialAction(null)}>{credentialError && <div className="routine-conflict-alert" role="alert"><AlertTriangle aria-hidden="true" size={20} /><div><strong>Não foi possível concluir</strong><span>{credentialError}</span></div></div>}<label>Senha da conta<input autoComplete="current-password" type="password" value={credentialPassword} onChange={(event) => setCredentialPassword(event.target.value)} /></label><div className="modal-actions"><button disabled={tokenBusy} onClick={() => setCredentialAction(null)}>Cancelar</button><button className={credentialAction === "issue" ? "primary" : "danger-confirm"} disabled={tokenBusy || !credentialPassword} onClick={async () => { setTokenBusy(true); setCredentialError(""); try { if (credentialAction === "issue") setToken(await onIssueToken(credentialPassword)); else { await onRevokeToken(credentialPassword); setToken(""); } setCredentialPassword(""); setCredentialAction(null); } catch (error) { setCredentialError(error instanceof Error ? error.message : "Tente novamente."); } finally { setTokenBusy(false); } }}>{credentialAction === "issue" ? "Gerar token" : "Revogar acesso"}</button></div></Modal>}{deleteOpen && <Modal title="Excluir computador?" description={`“${device.name}” será removido permanentemente. O agente perderá o acesso e precisará ser cadastrado novamente.`} onClose={() => !deleteBusy && setDeleteOpen(false)}>{deleteError && <div className="routine-conflict-alert" role="alert"><AlertTriangle aria-hidden="true" size={20} /><div><strong>Não foi possível excluir</strong><span>{deleteError}</span></div></div>}<label>Senha da conta<input autoComplete="current-password" type="password" value={deletePassword} onChange={(event) => setDeletePassword(event.target.value)} /></label><div className="modal-actions"><button disabled={deleteBusy} onClick={() => setDeleteOpen(false)}>Cancelar</button><button className="danger-confirm" disabled={deleteBusy || !deletePassword} onClick={async () => { setDeleteBusy(true); setDeleteError(""); try { await onDelete(deletePassword); setDeleteOpen(false); } catch (error) { setDeleteError(error instanceof Error ? error.message : "Tente novamente."); } finally { setDeleteBusy(false); } }}><Trash2 size={18} />{deleteBusy ? "Excluindo…" : "Excluir"}</button></div></Modal>}</section>;
}
