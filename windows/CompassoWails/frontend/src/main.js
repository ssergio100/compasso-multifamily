import './style.css';
import './responsive.css';
import brandMark from './assets/images/compasso-brand-mark.png';
import { WindowsAccounts, AddTime, Configure, InitialView, Synchronization, Settings } from '../wailsjs/go/main/App';

const icon = (name) => ({
  eye: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M2.5 12s3.5-6 9.5-6 9.5 6 9.5 6-3.5 6-9.5 6-9.5-6-9.5-6Z"/><circle cx="12" cy="12" r="2.8"/></svg>',
  info: '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M12 10.5v6M12 7.5h.01"/></svg>',
  gear: '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2.83 2.83-.06-.06a1.7 1.7 0 0 0-1.88-.34 1.7 1.7 0 0 0-1.03 1.56V21h-4v-.08A1.7 1.7 0 0 0 9 19.37a1.7 1.7 0 0 0-1.88.34l-.06.06-2.83-2.83.06-.06A1.7 1.7 0 0 0 4.63 15a1.7 1.7 0 0 0-1.55-1H3v-4h.08A1.7 1.7 0 0 0 4.63 9a1.7 1.7 0 0 0-.34-1.88l-.06-.06 2.83-2.83.06.06A1.7 1.7 0 0 0 9 4.63a1.7 1.7 0 0 0 1-1.55V3h4v.08A1.7 1.7 0 0 0 15 4.63a1.7 1.7 0 0 0 1.88-.34l.06-.06 2.83 2.83-.06.06A1.7 1.7 0 0 0 19.37 9a1.7 1.7 0 0 0 1.55 1H21v4h-.08A1.7 1.7 0 0 0 19.4 15Z"/></svg>',
  chevron: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m7 10 5 5 5-5"/></svg>',
}[name]);

let duration = 30;
let windowsAccounts = [];
let agentSettings = null;
let serviceAvailable = false;
let submitting = false;
let configuring = false;
let synchronizationTimer = null;
const brand = (large = false) => `<div class="brand ${large ? 'brand-large' : ''}"><img src="${brandMark}" alt=""><strong>Compasso</strong></div>`;
const escapeHtml = (value) => String(value || '').replace(/[&<>"']/g, character => ({
  '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
}[character]));

function connectionView(state) {
  const tone = state.available ? (state.online ? 'online' : 'offline') : 'unavailable';
  const title = !state.available
    ? 'Serviço do agente indisponível'
    : state.status === 'online'
      ? 'Servidor conectado'
      : state.status === 'checking'
        ? 'Aguardando a primeira resposta do servidor…'
        : 'Servidor sem comunicação';
  const detail = state.detail && state.detail !== title
    ? `<small>${escapeHtml(state.detail)}</small>`
    : '';
  return `<div class="connection ${tone}"><span></span><div><strong>${title}</strong>${detail}</div></div>`;
}

function addTimeView() {
  return `<main class="screen add-time">
    <header class="app-brand">${brand()}</header>
    <section class="content">
      <h1>Adicionar tempo</h1>
      <p class="lead">Escolha o período e confirme com a senha do responsável.</p>
      <div class="connection-slot">${connectionView(window.__syncState || {})}</div>
      <div class="durations" role="radiogroup" aria-label="Período">
        ${[15, 30, 60, 120].map(value => `<button class="duration ${value === duration ? 'selected' : ''}" role="radio" aria-checked="${value === duration}" data-duration="${value}">${value} min</button>`).join('')}
      </div>
      <label class="field-label" for="parent-password">Senha do responsável</label>
      <div class="password-field"><input id="parent-password" type="password" placeholder="Digite sua senha" autocomplete="current-password"><button class="reveal" type="button" aria-label="Mostrar senha">${icon('eye')}</button></div>
      <div class="feedback" role="status" aria-live="polite"></div>
      <button class="primary add-button" type="button" disabled>Adicionar ${duration} minutos</button>
      <p class="privacy">${icon('info')}<em>A senha <strong>não será</strong> armazenada.</em></p>
    </section>
    <footer><button class="link-button settings-link" type="button">${icon('gear')} Configurações</button></footer>
  </main>`;
}

function settingsView() {
  const configured = agentSettings && agentSettings.configured;
  const deviceId = agentSettings && agentSettings.deviceId ? agentSettings.deviceId : '';
  const serverUrl = agentSettings && agentSettings.serverUrl ? agentSettings.serverUrl : 'https://apifamily.smresume.com';
  const configuredSID = agentSettings && agentSettings.controlledUserSid ? agentSettings.controlledUserSid : '';
  const selectedAccount = windowsAccounts.find(candidate => candidate.sid === configuredSID)
    || windowsAccounts.find(candidate => candidate.current)
    || windowsAccounts[0];
  const accountOptions = windowsAccounts.length
    ? windowsAccounts.map(candidate => `<option value="${escapeHtml(candidate.sid)}" ${selectedAccount && candidate.sid === selectedAccount.sid ? 'selected' : ''}>${escapeHtml(candidate.name)}</option>`).join('')
    : '<option value="">Nenhuma conta local disponível</option>';
  const confirmationName = selectedAccount ? selectedAccount.name : 'selecionada';
  return `<main class="screen settings"><section class="settings-content">
    ${brand(true)}
    <h1>Configurações do Compasso</h1>
    <p class="settings-lead">Conecte este computador ao seu painel familiar.</p>
    <div class="status ${configured ? 'ok' : ''}">${configured ? 'Configurado' : 'Ainda não configurado'}</div>
    <div class="connection-slot settings-connection">${connectionView(window.__syncState || {})}</div>
    <form id="settings-form">
      <fieldset><legend>Conta que será controlada</legend>
        <label for="windows-account">Conta Windows</label>
        <div class="select-wrap"><select id="windows-account" ${windowsAccounts.length ? '' : 'disabled'}>${accountOptions}</select>${icon('chevron')}</div>
        <label class="consent"><input id="controlled-account-confirmation" type="checkbox" required><span>Confirmo que a conta “${escapeHtml(confirmationName)}” poderá ter a sessão bloqueada pelo Compasso.</span></label>
      </fieldset>
      <fieldset><legend>Conexão com o painel</legend>
        <label for="server">Endereço do servidor</label><input id="server" type="url" value="${escapeHtml(serverUrl)}" required>
        <label for="device-id">Identificador do dispositivo</label><input id="device-id" value="${escapeHtml(deviceId)}" placeholder="Cole o identificador gerado no painel" required>
        <label for="token">Token do dispositivo</label>
        <div class="token-field"><input id="token" type="password" autocomplete="new-password" placeholder="${configured ? 'Informe novamente para salvar' : 'Cole o token gerado no painel'}" required><button class="show-token" type="button">Mostrar</button></div>
        <p class="token-note">O token fica protegido neste computador e nunca é exibido novamente.</p>
      </fieldset>
      <div class="feedback settings-feedback" role="status" aria-live="polite">${windowsAccounts.length ? '' : 'Nenhuma conta Windows local elegível foi encontrada.'}</div>
      <button class="primary save-button" type="submit" disabled>Salvar e conectar</button>
    </form>
    <button class="back-button" type="button">Voltar para adicionar tempo</button>
  </section></main>`;
}

async function openSettings() {
  const [settings, accounts] = await Promise.all([
    Settings().catch(() => null),
    WindowsAccounts().catch(() => []),
  ]);
  agentSettings = settings;
  windowsAccounts = Array.isArray(accounts) ? accounts : [];
  render('settings');
}

function showFeedback(tone, text) {
  const feedback = document.querySelector('.feedback');
  if (!feedback) return;
  feedback.className = `feedback ${tone}`;
  feedback.textContent = text;
}

function updateSubmitButton() {
  const button = document.querySelector('.add-button');
  if (!button) return;
  button.disabled = submitting || !serviceAvailable;
  button.textContent = submitting ? 'Verificando…' : `Adicionar ${duration} minutos`;
}

async function submitAddTime() {
  const password = document.querySelector('#parent-password');
  if (!password || submitting || !serviceAvailable) return;
  if (!password.value) {
    showFeedback('error', 'Informe a senha do responsável.');
    password.focus();
    return;
  }

  submitting = true;
  updateSubmitButton();
  showFeedback('', 'Verificando…');
  try {
    const result = await AddTime(password.value, duration);
    if (result && result.ok) {
      const grantedMinutes = Math.round(result.bonusSeconds / 60);
      showFeedback('success', `Tempo adicionado: ${grantedMinutes} minutos.`);
    } else {
      showFeedback('error', (result && result.message) || 'Não foi possível adicionar tempo.');
    }
  } catch (error) {
    showFeedback('error', 'O serviço Compasso está indisponível. Abra as configurações para revisar a configuração.');
  } finally {
    password.value = '';
    submitting = false;
    updateSubmitButton();
  }
}

async function refreshSyncState() {
  try {
    const state = await Synchronization();
    window.__syncState = state || {};
  } catch (error) {
    window.__syncState = { available: false, detail: 'Serviço do Compasso indisponível' };
  }
  serviceAvailable = Boolean(window.__syncState && window.__syncState.available);
  const slot = document.querySelector('.connection-slot');
  if (slot && window.__syncState) slot.innerHTML = connectionView(window.__syncState);
  const settingsStatus = document.querySelector('.settings .status');
  if (settingsStatus && serviceAvailable) {
    const settings = await Settings().catch(() => null);
    if (settings && (settings.serverUrl || settings.deviceId || settings.controlledUserSid)) {
      agentSettings = settings;
      settingsStatus.textContent = settings.configured ? 'Configurado' : 'Ainda não configurado';
      settingsStatus.classList.toggle('ok', Boolean(settings.configured));
    }
  }
  updateSubmitButton();
}

function startSynchronizationRefresh() {
  if (synchronizationTimer !== null) return;
  synchronizationTimer = window.setInterval(refreshSyncState, 2000);
}

function bindAddTime() {
  document.querySelectorAll('.duration').forEach(button => button.addEventListener('click', () => {
    duration = Number(button.dataset.duration);
    document.querySelectorAll('.duration').forEach(option => {
      const selected = Number(option.dataset.duration) === duration;
      option.classList.toggle('selected', selected);
      option.setAttribute('aria-checked', String(selected));
    });
    updateSubmitButton();
  }));
  const password = document.querySelector('#parent-password');
  document.querySelector('.reveal').addEventListener('click', event => {
    const show = password.type === 'password';
    password.type = show ? 'text' : 'password';
    event.currentTarget.setAttribute('aria-label', show ? 'Ocultar senha' : 'Mostrar senha');
  });
  document.querySelector('.settings-link').addEventListener('click', async () => {
    await openSettings();
  });
  document.querySelector('.add-button').addEventListener('click', submitAddTime);
  password.addEventListener('keydown', event => { if (event.key === 'Enter') submitAddTime(); });
  refreshSyncState();
  startSynchronizationRefresh();
}

function bindSettings() {
  const token = document.querySelector('#token');
  const accountSelect = document.querySelector('#windows-account');
  const confirmation = document.querySelector('#controlled-account-confirmation');
  const saveButton = document.querySelector('.save-button');
  const updateConfirmationText = () => {
    const selected = windowsAccounts.find(candidate => candidate.sid === accountSelect.value);
    const label = document.querySelector('.consent span');
    if (label) label.textContent = `Confirmo que a conta “${selected ? selected.name : 'selecionada'}” poderá ter a sessão bloqueada pelo Compasso.`;
  };
  const updateSaveButton = () => {
    saveButton.disabled = configuring || !accountSelect.value || !confirmation.checked;
    saveButton.textContent = configuring ? 'Aguardando o servidor…' : 'Salvar e conectar';
  };
  document.querySelector('.show-token').addEventListener('click', event => {
    const show = token.type === 'password';
    token.type = show ? 'text' : 'password';
    event.currentTarget.textContent = show ? 'Ocultar' : 'Mostrar';
  });
  accountSelect.addEventListener('change', () => {
    confirmation.checked = false;
    updateConfirmationText();
    updateSaveButton();
  });
  confirmation.addEventListener('change', updateSaveButton);
  document.querySelector('.back-button').addEventListener('click', () => render('add'));
  document.querySelector('#settings-form').addEventListener('submit', async event => {
    event.preventDefault();
    if (configuring) return;
    configuring = true;
    updateSaveButton();
    showFeedback('', 'Aguardando autorização administrativa e a primeira resposta do servidor…');
    try {
	  const selectedAccount = windowsAccounts.find(candidate => candidate.sid === accountSelect.value);
      const result = await Configure(
        document.querySelector('#server').value,
        document.querySelector('#device-id').value,
        token.value,
		selectedAccount ? selectedAccount.name : '',
        accountSelect.value,
        confirmation.checked,
      );
      if (result && result.ok) {
        agentSettings = result.settings || agentSettings;
        const status = document.querySelector('.status');
        status.textContent = 'Configurado';
        status.classList.add('ok');
        showFeedback('success', result.message || 'Configuração concluída e servidor conectado.');
      } else {
        showFeedback('error', (result && result.message) || 'Não foi possível configurar o Compasso.');
      }
    } catch (error) {
      showFeedback('error', 'Não foi possível configurar o Compasso. Verifique os dados e tente novamente.');
    } finally {
      token.value = '';
      token.type = 'password';
      document.querySelector('.show-token').textContent = 'Mostrar';
      configuring = false;
      updateSaveButton();
      refreshSyncState();
    }
  });
  updateConfirmationText();
  updateSaveButton();
  refreshSyncState();
  startSynchronizationRefresh();
}

function render(view = 'add') {
  document.querySelector('#app').innerHTML = view === 'settings' ? settingsView() : addTimeView();
  view === 'settings' ? bindSettings() : bindAddTime();
}

InitialView()
  .then(view => view === 'settings' ? openSettings() : render('add'))
  .catch(() => render('add'));
