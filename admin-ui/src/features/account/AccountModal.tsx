import { AlertTriangle, KeyRound, Mail, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { api } from "../../api";
import { Modal } from "../../components";

export function AccountModal({ onClose, onSignedOut }: { onClose: () => void; onSignedOut: () => void }) {
  const [account, setAccount] = useState<{ email: string; family_name: string } | null>(null);
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [newEmail, setNewEmail] = useState("");
  const [deletePassword, setDeletePassword] = useState("");
  const [familyConfirmation, setFamilyConfirmation] = useState("");
  const [busy, setBusy] = useState<"email" | "password" | "delete" | null>(null);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    api.account().then(setAccount).catch((cause) => setError(cause instanceof Error ? cause.message : "Não foi possível carregar a conta."));
  }, []);

  const begin = (operation: "email" | "password" | "delete") => {
    setBusy(operation);
    setMessage("");
    setError("");
  };

  return <Modal title="Minha conta" description={account ? `Família ${account.family_name} · ${account.email || "e-mail ainda não confirmado"}` : "Carregando dados da conta…"} onClose={() => !busy && onClose()}>
    {error && <div className="routine-conflict-alert" role="alert"><AlertTriangle aria-hidden="true" size={20} /><div><strong>Não foi possível concluir</strong><span>{error}</span></div></div>}
    {message && <p className="account-success" role="status">{message}</p>}
    {account && <div className="account-sections">
      <form className="modal-form" onSubmit={async (event) => { event.preventDefault(); begin("email"); try { const result = await api.changeAccountEmail(currentPassword, newEmail); setMessage(result.message); setCurrentPassword(""); setNewEmail(""); } catch (cause) { setError(cause instanceof Error ? cause.message : "Não foi possível alterar o e-mail."); } finally { setBusy(null); } }}><h3><Mail size={18} />Alterar e-mail</h3><label>Novo e-mail<input autoComplete="email" inputMode="email" type="email" value={newEmail} onChange={(event) => setNewEmail(event.target.value)} /></label><label>Senha atual<input autoComplete="current-password" type="password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} /></label><button className="primary-button" disabled={Boolean(busy) || !newEmail || !currentPassword}>Enviar confirmação</button></form>
      <form className="modal-form" onSubmit={async (event) => { event.preventDefault(); if (newPassword.length < 12 || newPassword !== confirmation) { setError("Use ao menos 12 caracteres e repita a mesma senha."); return; } begin("password"); try { await api.changeAccountPassword(currentPassword, newPassword, confirmation); onSignedOut(); } catch (cause) { setError(cause instanceof Error ? cause.message : "Não foi possível alterar a senha."); } finally { setBusy(null); } }}><h3><KeyRound size={18} />Alterar senha</h3><label>Senha atual<input autoComplete="current-password" type="password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} /></label><label>Nova senha<input autoComplete="new-password" minLength={12} type="password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} /></label><label>Confirmar nova senha<input autoComplete="new-password" minLength={12} type="password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} /></label><button className="primary-button" disabled={Boolean(busy) || !currentPassword || newPassword.length < 12 || newPassword !== confirmation}>Alterar e sair</button></form>
      <form className="modal-form account-danger" onSubmit={async (event) => { event.preventDefault(); begin("delete"); try { await api.deleteAccount(deletePassword, familyConfirmation); onSignedOut(); } catch (cause) { setError(cause instanceof Error ? cause.message : "Não foi possível excluir a conta."); } finally { setBusy(null); } }}><h3><Trash2 size={18} />Excluir família</h3><p>Remove permanentemente a conta, os computadores e todas as configurações.</p><label>Digite “{account.family_name}”<input autoComplete="off" value={familyConfirmation} onChange={(event) => setFamilyConfirmation(event.target.value)} /></label><label>Senha atual<input autoComplete="current-password" type="password" value={deletePassword} onChange={(event) => setDeletePassword(event.target.value)} /></label><button className="danger-button" disabled={Boolean(busy) || familyConfirmation !== account.family_name || !deletePassword}><Trash2 size={18} />Excluir permanentemente</button></form>
    </div>}
  </Modal>;
}
