import { ChevronDown, LogOut, Plus, UserRoundCheck } from "lucide-react";
import { useRef } from "react";
import { Brand } from "../../components";
import type { Device } from "../../types";
import { DeviceAvatar } from "../../visuals";
import { avatarKeyFor, DeviceState, deviceVisualState } from "./devicePresentation";

interface NavigationProps {
  devices: Device[];
  selected: Device;
  onSelect: (deviceId: string) => void;
  onAccount: () => void;
  onLogout: () => void;
}

export function DeviceRail({ devices, selected, onSelect, onAdd, onAccount, onLogout }: NavigationProps & { onAdd: () => void }) {
  return <aside className="device-rail"><Brand /><div className="rail-title">Computadores <button aria-label="Adicionar computador" onClick={onAdd}><Plus size={18} /></button></div>{devices.map((device) => <button className={`${device.id === selected.id ? "active " : ""}device-${deviceVisualState(device)}`} key={device.id} onClick={() => onSelect(device.id)}><DeviceAvatar avatarKey={avatarKeyFor(device)} name={device.name} /><span><strong>{device.name}</strong><DeviceState device={device} /></span></button>)}<button className="add-device" onClick={onAdd}><Plus size={17} />Adicionar computador</button><div className="rail-account"><UserRoundCheck size={19} /><button className="account-link" onClick={onAccount}><small>Sessão</small><strong>Minha conta</strong></button><button aria-label="Sair" onClick={onLogout}><LogOut size={18} />Sair</button></div></aside>;
}

export function MobileDeviceHeader({ devices, selected, onSelect, onAccount, onLogout }: NavigationProps) {
  const devicePicker = useRef<HTMLDetailsElement>(null);
  const selectDevice = (deviceId: string) => {
    onSelect(deviceId);
    devicePicker.current?.removeAttribute("open");
  };

  return <header className="mobile-header"><div className="mobile-account"><Brand /><span><button onClick={onAccount}><UserRoundCheck size={17} />Conta</button><button onClick={onLogout}><LogOut size={17} />Sair</button></span></div><details ref={devicePicker}><summary><DeviceAvatar avatarKey={avatarKeyFor(selected)} name={selected.name} /><span><strong>{selected.name}</strong><DeviceState device={selected} /></span><ChevronDown size={19} /></summary><div>{devices.map((device) => <button key={device.id} onClick={() => selectDevice(device.id)}><DeviceAvatar avatarKey={avatarKeyFor(device)} name={device.name} /><span>{device.name}<DeviceState device={device} /></span></button>)}</div></details></header>;
}
