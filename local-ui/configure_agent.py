#!/usr/bin/env python3
"""Graphical first-run assistant for configuring the Compasso agent."""

import argparse
import getpass
import json
import os
import pwd
import subprocess
import threading
from urllib.parse import urlparse

import gi

gi.require_version("Gtk", "4.0")
from gi.repository import Gio, GLib, Gtk


DEFAULT_SERVER_URL = "https://apifamily.smresume.com"
SETUP_MARKER_PATH = "/etc/tempo-agent/setup-complete"
PRIVILEGED_HELPER_PATH = "/usr/sbin/tempo-agent-configure"
PKEXEC_PATH = "/usr/bin/pkexec"
BUS_NAME = "br.com.tempo.Agent"
OBJECT_PATH = "/br/com/tempo/Agent"
INTERFACE_NAME = "br.com.tempo.Agent"
APPLICATION_ID = "br.com.compasso.AgentSetup"
APPLICATION_ICON = "br.com.compasso.Compasso"


def synchronization_status_text(state, detail=""):
    if state == "online":
        return "● Servidor conectado"
    if state == "checking":
        return "◌ Aguardando a primeira resposta do servidor…"
    if state == "offline":
        explanation = detail or (
            "A comunicação falhou. Revise o endereço, o dispositivo e o token."
        )
        return f"⚠ Servidor sem comunicação\n{explanation}"
    return "○ Estado de comunicação desconhecido"


def synchronization_report(proxy):
    """Read the detailed report and remain compatible with older agents."""
    try:
        result = proxy.call_sync(
            "GetSynchronizationReport",
            None,
            Gio.DBusCallFlags.NONE,
            1000,
            None,
        )
        return result.unpack()
    except GLib.Error as error:
        remote_name = Gio.DBusError.get_remote_error(error) or ""
        if not remote_name.endswith("UnknownMethod"):
            raise
    result = proxy.call_sync(
        "GetSynchronizationStatus",
        None,
        Gio.DBusCallFlags.NONE,
        1000,
        None,
    )
    return result.unpack()[0], ""


def available_controlled_users():
    """Return regular interactive account names in deterministic order."""
    candidates = []
    for account in pwd.getpwall():
        if account.pw_uid < 1000 or account.pw_uid == 65534:
            continue
        if account.pw_shell.endswith(("/false", "/nologin")):
            continue
        candidates.append(account.pw_name)
    current_user = getpass.getuser()
    unique_names = sorted(set(candidates))
    if current_user in unique_names:
        unique_names.remove(current_user)
        unique_names.insert(0, current_user)
    return unique_names


def initial_controlled_user(controlled_users):
    """Return the account that the dropdown visibly selects on first open."""
    return controlled_users[0] if controlled_users else ""


def validate_form(controlled_user, server_url, device_id, device_token):
    if not controlled_user:
        return "Escolha a conta Linux que será controlada."
    parsed_url = urlparse(server_url.strip())
    if parsed_url.scheme not in ("http", "https") or not parsed_url.hostname:
        return "Informe um endereço válido para o servidor."
    if parsed_url.username or parsed_url.password or parsed_url.query or parsed_url.fragment:
        return "O endereço do servidor não pode conter credenciais, consulta ou fragmento."
    loopback_hosts = {"localhost", "127.0.0.1", "::1"}
    if parsed_url.scheme == "http" and parsed_url.hostname not in loopback_hosts:
        return "Use HTTPS quando o servidor estiver em outra máquina."
    if not device_id.strip():
        return "Informe o identificador do dispositivo (device_id)."
    if not device_token.strip():
        return "Informe o token do dispositivo (device_token)."
    return None


def controlled_user_confirmation_text(controlled_user):
    if not controlled_user:
        return "Escolha uma conta antes de confirmar."
    return (
        f'Confirmo que a conta “{controlled_user}” será controlada '
        "e poderá ter a sessão bloqueada."
    )


def run_privileged_configuration(configuration):
    completed = subprocess.run(
        [PKEXEC_PATH, PRIVILEGED_HELPER_PATH],
        input=json.dumps(configuration),
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        timeout=90,
        check=False,
    )
    if completed.returncode == 0:
        return None
    if completed.returncode in (126, 127):
        return "A autorização administrativa foi cancelada."
    error_message = completed.stderr.strip()
    if "must use HTTPS" in error_message:
        return "Use HTTPS quando o servidor estiver em outra máquina."
    if "does not exist" in error_message:
        return "A conta Linux selecionada não existe."
    if "did not remain active" in error_message:
        return "A configuração foi recusada porque o serviço não permaneceu ativo."
    if "server rejected device credentials" in error_message:
        return (
            "O servidor recusou o device_id ou o device_token. "
            "Gere uma nova credencial no painel e tente novamente."
        )
    if "server communication was not confirmed" in error_message:
        return (
            "O agente foi iniciado, mas o servidor ainda não confirmou a comunicação. "
            "Verifique a conexão e tente novamente."
        )
    return "Não foi possível configurar o Compasso. Verifique os dados e tente novamente."


class AgentSetupWindow(Gtk.ApplicationWindow):
    def __init__(self, application):
        super().__init__(application=application, title="Compasso — Configurar agente")
        self.set_icon_name(APPLICATION_ICON)
        self.set_default_size(540, 520)
        self.set_resizable(False)
        self.status_proxy = None

        content = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=12)
        content.set_margin_top(24)
        content.set_margin_bottom(24)
        content.set_margin_start(24)
        content.set_margin_end(24)
        self.set_child(content)

        title = Gtk.Label(label="Configurar o agente Compasso")
        title.add_css_class("title-2")
        title.set_halign(Gtk.Align.START)
        content.append(title)

        description = Gtk.Label(
            label=(
                "Informe os dados criados no painel administrativo. "
                "O token será armazenado em um arquivo acessível somente pelo sistema."
            )
        )
        description.set_wrap(True)
        description.set_halign(Gtk.Align.START)
        content.append(description)

        self.server_connection_status = Gtk.Label(
            label=synchronization_status_text("checking")
        )
        self.server_connection_status.set_halign(Gtk.Align.START)
        content.append(self.server_connection_status)
        GLib.idle_add(self._refresh_server_status)
        GLib.timeout_add_seconds(2, self._refresh_server_status)

        controlled_users = available_controlled_users()
        selected_controlled_user = initial_controlled_user(controlled_users)
        self.controlled_user_names = controlled_users
        content.append(self._field_label("Conta Linux controlada"))
        self.controlled_user = Gtk.DropDown.new_from_strings(controlled_users)
        if controlled_users:
            self.controlled_user.set_selected(0)
        else:
            self.controlled_user.set_selected(Gtk.INVALID_LIST_POSITION)
        self.controlled_user.set_sensitive(bool(controlled_users))
        content.append(self.controlled_user)

        self.controlled_user_confirmation = Gtk.CheckButton(
            label=controlled_user_confirmation_text(
                selected_controlled_user
            )
        )
        self.controlled_user_confirmation.set_sensitive(bool(controlled_users))
        self.controlled_user_confirmation.connect("toggled", self._confirmation_toggled)
        content.append(self.controlled_user_confirmation)
        self.controlled_user.connect("notify::selected", self._controlled_user_changed)

        content.append(self._field_label("Endereço do servidor"))
        self.server_url = Gtk.Entry()
        self.server_url.set_text(DEFAULT_SERVER_URL)
        self.server_url.set_input_purpose(Gtk.InputPurpose.URL)
        content.append(self.server_url)

        content.append(self._field_label("Identificador do dispositivo (device_id)"))
        self.device_id = Gtk.Entry()
        content.append(self.device_id)

        content.append(self._field_label("Token do dispositivo (device_token)"))
        self.device_token = Gtk.Entry()
        self.device_token.set_visibility(False)
        self.device_token.set_input_purpose(Gtk.InputPurpose.PASSWORD)
        self.device_token.connect("activate", self._submit)
        content.append(self.device_token)

        show_token = Gtk.CheckButton(label="Mostrar token")
        show_token.connect(
            "toggled", lambda button: self.device_token.set_visibility(button.get_active())
        )
        content.append(show_token)

        self.submit_button = Gtk.Button(label="Configurar e iniciar o Compasso")
        self.submit_button.add_css_class("suggested-action")
        self.submit_button.set_sensitive(False)
        self.submit_button.connect("clicked", self._submit)
        content.append(self.submit_button)

        self.status = Gtk.Label()
        self.status.set_wrap(True)
        self.status.set_halign(Gtk.Align.START)
        if not controlled_users:
            self.status.set_text("Nenhuma conta Linux comum foi encontrada nesta máquina.")
        content.append(self.status)

    @staticmethod
    def _field_label(text):
        label = Gtk.Label(label=text)
        label.set_halign(Gtk.Align.START)
        label.add_css_class("heading")
        return label

    def _selected_controlled_user(self):
        selected_index = self.controlled_user.get_selected()
        if selected_index >= len(self.controlled_user_names):
            return ""
        return self.controlled_user_names[selected_index]

    def _controlled_user_changed(self, _dropdown, _parameter):
        controlled_user = self._selected_controlled_user()
        self.controlled_user_confirmation.set_active(False)
        self.controlled_user_confirmation.set_label(
            controlled_user_confirmation_text(controlled_user)
        )
        self.controlled_user_confirmation.set_sensitive(bool(controlled_user))
        self._update_submit_sensitivity()

    def _confirmation_toggled(self, _button):
        self._update_submit_sensitivity()

    def _update_submit_sensitivity(self):
        self.submit_button.set_sensitive(
            bool(self._selected_controlled_user())
            and self.controlled_user_confirmation.get_active()
        )

    def _refresh_server_status(self):
        try:
            if self.status_proxy is None:
                self.status_proxy = Gio.DBusProxy.new_for_bus_sync(
                    Gio.BusType.SYSTEM,
                    Gio.DBusProxyFlags.DO_NOT_LOAD_PROPERTIES,
                    None,
                    BUS_NAME,
                    OBJECT_PATH,
                    INTERFACE_NAME,
                    None,
                )
            state, detail = synchronization_report(self.status_proxy)
            self.server_connection_status.set_text(
                synchronization_status_text(state, detail)
            )
        except GLib.Error:
            self.status_proxy = None
            self.server_connection_status.set_text("○ Serviço do agente indisponível")
        return GLib.SOURCE_CONTINUE

    def _submit(self, _widget):
        if not self.controlled_user_confirmation.get_active():
            self.status.set_text("Confirme explicitamente a conta que será controlada.")
            return
        configuration = {
            "controlled_user": self._selected_controlled_user(),
            "server_url": self.server_url.get_text().strip().rstrip("/"),
            "device_id": self.device_id.get_text().strip(),
            "device_token": self.device_token.get_text(),
        }
        error_message = validate_form(**configuration)
        if error_message:
            self.status.set_text(error_message)
            return

        self.submit_button.set_sensitive(False)
        self.status.set_text(
            "Aguardando autorização administrativa e a primeira resposta do servidor…"
        )
        self.server_connection_status.set_text(
            synchronization_status_text("checking")
        )
        worker = threading.Thread(
            target=self._configure_in_background,
            args=(configuration,),
            daemon=True,
        )
        worker.start()

    def _configure_in_background(self, configuration):
        try:
            error_message = run_privileged_configuration(configuration)
        except subprocess.TimeoutExpired:
            error_message = "A configuração demorou demais e foi cancelada."
        except OSError:
            error_message = "O componente de autorização administrativa não está disponível."
        finally:
            configuration["device_token"] = ""
        GLib.idle_add(self._configuration_finished, error_message)

    def _configuration_finished(self, error_message):
        self.device_token.set_text("")
        self._update_submit_sensitivity()
        if error_message:
            self.status.set_text(error_message)
            self._refresh_server_status()
            return GLib.SOURCE_REMOVE
        self.status.set_text(
            "Configuração concluída. O agente está ativo e o servidor respondeu com sucesso."
        )
        self._refresh_server_status()
        return GLib.SOURCE_REMOVE


class AgentSetupApplication(Gtk.Application):
    def __init__(self, first_run=False):
        super().__init__(application_id=APPLICATION_ID)
        self.first_run = first_run

    def do_activate(self):
        if self.first_run and os.path.exists(SETUP_MARKER_PATH):
            self.quit()
            return
        window = self.props.active_window
        if window is None:
            window = AgentSetupWindow(self)
        window.present()


def main(arguments=None):
    parser = argparse.ArgumentParser(description="Configurar o agente Compasso")
    parser.add_argument("--first-run", action="store_true")
    parsed_arguments = parser.parse_args(arguments)
    return AgentSetupApplication(first_run=parsed_arguments.first_run).run([])


if __name__ == "__main__":
    raise SystemExit(main())
