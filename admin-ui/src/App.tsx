import { Activity, CalendarDays, Plus, RefreshCw, Settings, SlidersHorizontal, SquareTerminal } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { api, remoteMode } from "./api";
import { Brand, Toast } from "./components";
import { CommunicationPage } from "./communication/CommunicationPage";
import { AccountModal } from "./features/account/AccountModal";
import { AdministrationPage } from "./features/administration/AdministrationPage";
import { LoginPage } from "./features/auth/LoginPage";
import { lastSeen } from "./features/common/format";
import { DeviceModal } from "./features/devices/DeviceModal";
import { DeviceRail, MobileDeviceHeader } from "./features/devices/DeviceNavigation";
import { avatarKeyFor, DeviceState, deviceIsBlockedForAction } from "./features/devices/devicePresentation";
import { LimitsPage } from "./features/limits/LimitsPage";
import { BonusModal } from "./features/now/BonusModal";
import { NowPage } from "./features/now/NowPage";
import { RoutineModal } from "./features/routines/RoutineModal";
import { RoutinesPage } from "./features/routines/RoutinesPage";
import { withoutId } from "./features/routines/routineSchedule";
import { useDeviceStream } from "./hooks/useDeviceStream";
import { useNotifications } from "./hooks/useNotifications";
import { mockDevices } from "./mock";
import { initialDevice, initialView, isAppNavigationState, writeNavigationState, type AppModal, type AppNavigationState } from "./navigation";
import type { Device, Routine, View } from "./types";
import { DeviceAvatar } from "./visuals";

const nav: { id: View; label: string; short?: string; Icon: typeof Activity }[] = [
  { id: "now", label: "Agora", Icon: Activity },
  { id: "limits", label: "Limites", Icon: SlidersHorizontal },
  { id: "routines", label: "Rotinas", Icon: CalendarDays },
  { id: "administration", label: "Administração", short: "Admin.", Icon: Settings },
  { id: "communication", label: "Atividade", short: "Ativ.", Icon: SquareTerminal },
];

function localID() {
  return typeof globalThis.crypto?.randomUUID === "function"
    ? globalThis.crypto.randomUUID()
    : `local-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function mockToken() {
  return Array.from(crypto.getRandomValues(new Uint8Array(43)), (byte) => "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"[byte % 64]).join("");
}

export function App() {
  const [devices, setDevices] = useState<Device[]>(remoteMode ? [] : mockDevices);
  const [selectedId, setSelectedId] = useState(() => initialDevice(remoteMode ? "" : mockDevices[0].id));
  const [view, setView] = useState<View>(() => initialView());
  const [authenticated, setAuthenticated] = useState(!remoteMode);
  const [checking, setChecking] = useState(remoteMode);
  const [loading, setLoading] = useState(remoteMode);
  const [modal, setModal] = useState<AppModal>(null);
  const [editingRoutine, setEditingRoutine] = useState<Routine | null>(null);
  const navigationReady = useRef(false);
  const devicesRef = useRef(devices);
  const navigationRef = useRef<AppNavigationState>({
    compassoNavigation: true, view, selectedId, modal: null, editingRoutineId: null,
  });
  const pendingOperations = useRef(new Set<string>());
  const selected = useMemo(() => devices.find((item) => item.id === selectedId) ?? devices[0], [devices, selectedId]);
  const { message, notify, setMessage } = useNotifications();
  const { streamState, streamGeneration, activityUpdate, communicationUpdate } = useDeviceStream({
    deviceId: selected?.id,
    enabled: remoteMode,
    setDevices,
    pendingOperations,
    notify,
  });

  devicesRef.current = devices;
  const applyNavigation = (state: AppNavigationState) => {
    navigationRef.current = state;
    setView(state.view);
    setSelectedId(state.selectedId);
    setModal(state.modal);
    const device = devicesRef.current.find((item) => item.id === state.selectedId);
    setEditingRoutine(state.editingRoutineId
      ? device?.routines.find((routine) => routine.id === state.editingRoutineId) ?? null
      : null);
  };
  const navigate = (change: Partial<Omit<AppNavigationState, "compassoNavigation">>, replace = false) => {
    const current = navigationRef.current;
    const next = { ...current, ...change };
    if (next.view === current.view && next.selectedId === current.selectedId
      && next.modal === current.modal && next.editingRoutineId === current.editingRoutineId) return;
    applyNavigation(next);
    if (navigationReady.current) writeNavigationState(next, replace);
  };
  const openModal = (nextModal: Exclude<AppModal, null>, routine?: Routine) => {
    navigate({ modal: nextModal, editingRoutineId: routine?.id ?? null });
  };
  const closeModal = () => {
    if (navigationReady.current && isAppNavigationState(window.history.state) && window.history.state.modal) {
      window.history.back();
      return;
    }
    navigate({ modal: null, editingRoutineId: null }, true);
  };

  useEffect(() => {
    const restoreNavigation = (event: PopStateEvent) => {
      if (isAppNavigationState(event.state)) applyNavigation(event.state);
    };
    window.addEventListener("popstate", restoreNavigation);
    return () => window.removeEventListener("popstate", restoreNavigation);
  }, []);

  useEffect(() => {
    if (checking || !authenticated || !selectedId || navigationReady.current) return;
    const initial = { ...navigationRef.current, view, selectedId, modal: null, editingRoutineId: null };
    applyNavigation(initial);
    writeNavigationState(initial, true);
    navigationReady.current = true;
  }, [authenticated, checking, selectedId, view]);

  const load = async () => {
    setLoading(true);
    try {
      const list = await api.devices();
      setDevices(list);
      if (list.length && !list.some((device) => device.id === navigationRef.current.selectedId)) {
        navigate({ selectedId: list[0].id }, true);
      }
    } catch (error) {
      notify(error instanceof Error ? error.message : "Falha ao carregar dados.");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (!remoteMode) return;
    api.session()
      .then((session) => {
        setAuthenticated(session.authenticated);
        if (session.authenticated) void load();
      })
      .catch(() => setAuthenticated(false))
      .finally(() => setChecking(false));
  }, []);

  const patchDevice = (change: (device: Device) => Device) => {
    setDevices((all) => all.map((item) => item.id === selected.id ? change(item) : item));
  };
  const command = async (name: string, local: (device: Device) => Device, success: string) => {
    try {
      if (remoteMode) {
        const confirmation = await api.command(selected.id, name);
        pendingOperations.current.add(confirmation.operation_id);
      }
      patchDevice(local);
      notify(success);
      if (remoteMode) await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "Comando não enviado.");
    }
  };
  const logout = async () => {
    if (remoteMode) await api.logout();
    setAuthenticated(false);
  };
  const signedOut = () => {
    setAuthenticated(false);
    setDevices([]);
    setModal(null);
    navigationReady.current = false;
  };

  if (checking) return <div className="center-state"><Brand /><span className="loader" />Verificando sessão…</div>;
  if (!authenticated) return <LoginPage onLogin={async (login, password) => { const session = await api.login(login, password); setAuthenticated(session.authenticated); if (session.authenticated) await load(); return session.authenticated; }} />;
  if (loading && !selected) return <div className="center-state"><Brand /><span className="loader" />Organizando seus computadores…</div>;
  if (!selected) return <div className="center-state"><Brand /><h1>Nenhum computador</h1><button className="primary-button" onClick={() => openModal("device")}><Plus size={18} />Adicionar computador</button><button className="center-link" onClick={() => openModal("account")}>Minha conta</button>{modal === "device" && <DeviceModal onClose={closeModal} onSubmit={async (name, avatarKey) => { if (remoteMode) await api.createDevice(name, avatarKey); else setDevices([{ ...mockDevices[1], id: localID(), name, avatar_key: avatarKey }]); closeModal(); if (remoteMode) await load(); }} />}{modal === "account" && <AccountModal onClose={closeModal} onSignedOut={signedOut} />}{message && <Toast>{message}</Toast>}</div>;

  return <div className="app-shell">
    <a className="skip" href="#workspace">Pular para o conteúdo</a>
    <DeviceRail devices={devices} selected={selected} onSelect={(deviceId) => navigate({ selectedId: deviceId, view: "now", modal: null, editingRoutineId: null })} onAdd={() => openModal("device")} onAccount={() => openModal("account")} onLogout={() => void logout()} />
    <aside className="main-nav"><div className="nav-title">Ações</div><nav>{nav.map(({ id, label, Icon }) => <button className={view === id ? "active" : ""} key={id} onClick={() => navigate({ view: id, modal: null, editingRoutineId: null })}><Icon size={20} />{label}</button>)}</nav></aside>
    <main id="workspace">
      <MobileDeviceHeader devices={devices} selected={selected} onSelect={(deviceId) => navigate({ selectedId: deviceId, modal: null, editingRoutineId: null })} onAccount={() => openModal("account")} onLogout={() => void logout()} />
      <header className="workspace-header"><div><DeviceAvatar avatarKey={avatarKeyFor(selected)} className="workspace-avatar" name={selected.name} /><span><h1>{selected.name}</h1><DeviceState device={selected} /></span></div><button onClick={() => void load()}><RefreshCw size={17} />{lastSeen(selected.last_seen_at)}</button></header>
      <div className={`workspace-body ${view === "communication" ? "communication-workspace" : ""}`}>
        {view === "now" && <NowPage device={selected} onBonus={() => openModal("bonus")} onPause={() => void command(selected.monitoring_paused ? "resume_monitoring" : "pause_monitoring", (device) => ({ ...device, monitoring_paused: !device.monitoring_paused, manual_block: false, actual_state: remoteMode ? device.actual_state : "unblocked", control_status: device.monitoring_paused ? remoteMode ? "resume_requested" : "active" : remoteMode ? "pause_requested" : "paused" }), selected.monitoring_paused ? "Retomada solicitada." : "Pausa solicitada.")} onBlock={() => { const blockedForAction = deviceIsBlockedForAction(selected); void command(blockedForAction ? "clear_manual_block" : "block_now", (device) => ({ ...device, monitoring_paused: false, manual_block: !blockedForAction, actual_state: blockedForAction ? remoteMode ? device.actual_state : "unblocked" : remoteMode ? device.actual_state : "blocked", control_status: blockedForAction ? remoteMode ? "unblock_requested" : "active" : remoteMode ? "block_requested" : "blocked" }), blockedForAction ? "Desbloqueio solicitado." : "Bloqueio solicitado."); }} />}
        {view === "limits" && <LimitsPage device={selected} onSave={async (weekly) => { if (remoteMode) await api.policy(selected.id, weekly, selected.warning_minutes); patchDevice((device) => ({ ...device, weekly_quota_seconds: weekly })); notify("Limites salvos."); if (remoteMode) await load(); }} />}
        {view === "routines" && <RoutinesPage device={selected} onNew={() => openModal("routine")} onEdit={(routine) => openModal("routine", routine)} onToggle={async (routine) => { const next = { ...routine, enabled: !routine.enabled }; if (remoteMode) await api.routine(selected.id, withoutId(next), routine.id); patchDevice((device) => ({ ...device, routines: device.routines.map((item) => item.id === routine.id ? next : item) })); notify(next.enabled ? "Rotina ativada." : "Rotina pausada."); }} onDelete={async (routine) => { if (remoteMode) await api.deleteRoutine(selected.id, routine.id); patchDevice((device) => ({ ...device, routines: device.routines.filter((item) => item.id !== routine.id) })); notify("Rotina removida."); }} />}
        {view === "administration" && <AdministrationPage device={selected} onSave={async (name, warning, avatarKey) => { if (remoteMode) { await api.rename(selected.id, name, avatarKey); await api.policy(selected.id, selected.weekly_quota_seconds, warning); await load(); } patchDevice((device) => ({ ...device, name, warning_minutes: warning, avatar_key: avatarKey })); notify("Informações salvas."); }} onPassword={async (password, confirmation) => { if (remoteMode) await api.updatePassword(selected.id, password, confirmation); patchDevice((device) => ({ ...device, password_set: true })); notify("Senha local atualizada."); if (remoteMode) await load(); }} onIssueToken={async (currentPassword) => { const result = remoteMode ? await api.issueToken(selected.id, currentPassword) : { device_id: selected.id, device_token: mockToken() }; notify("Novo token gerado."); return result.device_token; }} onRevokeToken={async (currentPassword) => { if (remoteMode) await api.revokeToken(selected.id, currentPassword); notify("Token revogado."); }} onDelete={async (currentPassword) => { if (remoteMode) await api.deleteDevice(selected.id, currentPassword); const remaining = devices.filter((device) => device.id !== selected.id); setDevices(remaining); navigate({ selectedId: remaining[0]?.id ?? "", view: "now", modal: null, editingRoutineId: null }, true); notify("Computador excluído."); }} />}
        {view === "communication" && <CommunicationPage activityUpdate={activityUpdate} communicationUpdate={communicationUpdate} deviceId={selected.id} deviceName={selected.name} streamGeneration={streamGeneration} streamState={streamState} />}
      </div>
      <footer className="source-footer"><a href="https://github.com/ssergio100/compasso" rel="noreferrer" target="_blank">Código-fonte</a><span aria-hidden="true">·</span><span>AGPL-3.0-or-later</span></footer>
    </main>
    <nav className="bottom-nav">{nav.map(({ id, label, short, Icon }) => <button className={view === id ? "active" : ""} key={id} onClick={() => navigate({ view: id, modal: null, editingRoutineId: null })}><Icon size={21} /><span>{short ?? label}</span></button>)}</nav>
    {modal === "bonus" && <BonusModal onClose={closeModal} onSubmit={async (minutes) => { if (!remoteMode) { patchDevice((device) => ({ ...device, bonus_seconds: device.bonus_seconds + minutes * 60, remaining_seconds: device.remaining_seconds + minutes * 60 })); closeModal(); notify(`${minutes} minutos adicionados.`); return; } const confirmation = await api.bonus(selected.id, minutes); pendingOperations.current.add(confirmation.operation_id); closeModal(); setMessage("Pedido guardado pelo servidor. Aguardando o computador confirmar."); }} />}
    {modal === "device" && <DeviceModal onClose={closeModal} onSubmit={async (name, avatarKey) => { let createdId = ""; if (remoteMode) { const created = await api.createDevice(name, avatarKey); createdId = created.id; await load(); } else { const device = { ...mockDevices[1], id: localID(), name, avatar_key: avatarKey }; createdId = device.id; setDevices((all) => [...all, device]); } navigate({ selectedId: createdId, modal: null, editingRoutineId: null }, true); notify("Computador adicionado."); }} />}
    {modal === "account" && <AccountModal onClose={closeModal} onSignedOut={signedOut} />}
    {modal === "routine" && <RoutineModal initial={editingRoutine ?? undefined} routines={selected.routines.filter((routine) => routine.id !== editingRoutine?.id)} onClose={closeModal} onSubmit={async (draft) => { const routineId = editingRoutine?.id; const saved = remoteMode ? await api.routine(selected.id, draft, routineId) : { id: routineId ?? localID() }; const savedId = routineId ?? saved.id; if (remoteMode) await load(); patchDevice((device) => ({ ...device, routines: device.routines.some((routine) => routine.id === savedId) ? device.routines.map((routine) => routine.id === savedId ? { ...draft, id: savedId } : routine) : [...device.routines, { ...draft, id: savedId || localID() }] })); closeModal(); notify(routineId ? "Rotina atualizada." : "Rotina criada."); }} />}
    {message && <Toast>{message}</Toast>}
  </div>;
}
