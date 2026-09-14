import { useEffect, useRef, useState } from "react";
import { api } from "../../api";
import { Brand } from "../../components";

type Mode = "login" | "register" | "forgot" | "sent" | "reset" | "result";

export function LoginPage({ onLogin }: { onLogin: (login: string, password: string) => Promise<boolean> }) {
  const location = new URL(window.location.href);
  const action = location.searchParams.get("account_action");
  const token = location.searchParams.get("token") ?? "";
  const [mode, setMode] = useState<Mode>(action === "reset-password" && token ? "reset" : action && token ? "result" : "login");
  const [familyName, setFamilyName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState(action && token ? "Confirmando…" : "");
  const [sentKind, setSentKind] = useState<"confirmation" | "reset">("confirmation");
  const [busy, setBusy] = useState(false);
  const confirmationStarted = useRef(false);

  const clearAccountLink = () => {
    const clean = new URL(window.location.href);
    clean.searchParams.delete("account_action");
    clean.searchParams.delete("token");
    window.history.replaceState(window.history.state, "", clean);
  };

  useEffect(() => {
    if (!token || confirmationStarted.current || (action !== "confirm" && action !== "confirm-email")) return;
    confirmationStarted.current = true;
    const confirm = action === "confirm-email" ? api.confirmEmailChange(token) : api.confirmRegistration(token);
    confirm.then((result) => setMessage(result.message)).catch((cause) => {
      setError(cause instanceof Error ? cause.message : "Não foi possível confirmar este link.");
      setMessage("");
    }).finally(clearAccountLink);
  }, [action, token]);

  const switchMode = (next: Mode) => {
    setMode(next);
    setError("");
    setMessage("");
    setPassword("");
    setConfirmation("");
  };

  const title = mode === "register" ? "Criar conta" : mode === "forgot" ? "Recuperar senha" : mode === "reset" ? "Nova senha" : mode === "sent" ? "Verifique seu e-mail" : mode === "result" ? "Confirmação" : "Acesso administrativo";
  const description = mode === "register" ? "Crie o espaço seguro da sua família." : mode === "forgot" ? "Enviaremos um link se a conta existir." : mode === "reset" ? "Escolha uma senha com pelo menos 12 caracteres." : mode === "sent" ? "As instruções foram enviadas quando o endereço é elegível." : mode === "result" ? "O link foi processado abaixo." : "Administre os computadores da sua família.";

  return <main className="login"><section><Brand /><h1>{title}</h1><p>{description}</p>
    {mode === "login" && <form onSubmit={async (event) => { event.preventDefault(); setBusy(true); setError(""); try { if (!await onLogin(email, password)) setError("E-mail ou senha inválidos."); } catch (cause) { setError(cause instanceof Error ? cause.message : "Não foi possível entrar."); } finally { setBusy(false); } }}><label>E-mail ou acesso antigo<input autoComplete="username" value={email} onChange={(event) => setEmail(event.target.value)} /></label><label>Senha<input autoComplete="current-password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} /></label>{error && <p className="error" role="alert">{error}</p>}<button className="primary-button" disabled={busy || !email || !password}>{busy ? "Entrando…" : "Entrar"}</button><div className="login-links"><button type="button" onClick={() => switchMode("forgot")}>Esqueci a senha</button><button type="button" onClick={() => switchMode("register")}>Criar conta</button></div></form>}
    {mode === "register" && <form onSubmit={async (event) => { event.preventDefault(); setError(""); if (password.length < 12 || password !== confirmation) { setError("Use ao menos 12 caracteres e repita a mesma senha."); return; } setBusy(true); try { await api.register(familyName.trim(), email, password, confirmation); setSentKind("confirmation"); switchMode("sent"); } catch (cause) { setError(cause instanceof Error ? cause.message : "Não foi possível solicitar o cadastro."); } finally { setBusy(false); } }}><label>Nome da família<input autoComplete="organization" maxLength={120} value={familyName} onChange={(event) => setFamilyName(event.target.value)} /></label><label>E-mail<input autoComplete="email" inputMode="email" type="email" value={email} onChange={(event) => setEmail(event.target.value)} /></label><label>Senha<input autoComplete="new-password" minLength={12} type="password" value={password} onChange={(event) => setPassword(event.target.value)} /></label><label>Confirmar senha<input autoComplete="new-password" minLength={12} type="password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} /></label>{error && <p className="error" role="alert">{error}</p>}<button className="primary-button" disabled={busy || !familyName.trim() || !email || password.length < 12 || password !== confirmation}>{busy ? "Solicitando…" : "Criar conta"}</button><div className="login-links"><button type="button" onClick={() => switchMode("login")}>Já tenho conta</button></div></form>}
    {mode === "forgot" && <form onSubmit={async (event) => { event.preventDefault(); setBusy(true); setError(""); try { await api.requestPasswordReset(email); setSentKind("reset"); switchMode("sent"); } catch (cause) { setError(cause instanceof Error ? cause.message : "Não foi possível solicitar a recuperação."); } finally { setBusy(false); } }}><label>E-mail<input autoComplete="email" inputMode="email" type="email" value={email} onChange={(event) => setEmail(event.target.value)} /></label>{error && <p className="error" role="alert">{error}</p>}<button className="primary-button" disabled={busy || !email}>{busy ? "Solicitando…" : "Enviar instruções"}</button><div className="login-links"><button type="button" onClick={() => switchMode("login")}>Voltar</button></div></form>}
    {mode === "reset" && <form onSubmit={async (event) => { event.preventDefault(); setError(""); if (password.length < 12 || password !== confirmation) { setError("Use ao menos 12 caracteres e repita a mesma senha."); return; } setBusy(true); try { const result = await api.confirmPasswordReset(token, password, confirmation); setMessage(result.message); clearAccountLink(); setMode("result"); } catch (cause) { setError(cause instanceof Error ? cause.message : "O link expirou ou já foi usado."); } finally { setBusy(false); } }}><label>Nova senha<input autoComplete="new-password" minLength={12} type="password" value={password} onChange={(event) => setPassword(event.target.value)} /></label><label>Confirmar senha<input autoComplete="new-password" minLength={12} type="password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} /></label>{error && <p className="error" role="alert">{error}</p>}<button className="primary-button" disabled={busy || password.length < 12 || password !== confirmation}>{busy ? "Alterando…" : "Alterar senha"}</button></form>}
    {mode === "sent" && <div className="login-result"><p>{sentKind === "confirmation" ? "Abra a mensagem para confirmar a conta. O link vale por 24 horas." : "Abra a mensagem para definir uma nova senha. O link vale por 30 minutos."}</p>{sentKind === "confirmation" && <button disabled={busy} onClick={async () => { setBusy(true); setError(""); try { await api.resendConfirmation(email); setMessage("Se o cadastro estiver pendente, uma nova mensagem foi enviada."); } catch (cause) { setError(cause instanceof Error ? cause.message : "Não foi possível reenviar."); } finally { setBusy(false); } }}>Reenviar confirmação</button>}{message && <p className="success">{message}</p>}{error && <p className="error" role="alert">{error}</p>}<button className="primary-button" onClick={() => switchMode("login")}>Voltar para entrar</button></div>}
    {mode === "result" && <div className="login-result">{message && <p className="success">{message}</p>}{error && <p className="error" role="alert">{error}</p>}<button className="primary-button" onClick={() => switchMode("login")}>Ir para entrada</button></div>}
  </section></main>;
}
