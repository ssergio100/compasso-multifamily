import './style.css';
import './responsive.css';
import brandMark from './assets/images/compasso-brand-mark.png';
import { WindowsUser } from '../wailsjs/go/main/App';

const icon = (name) => ({
  eye: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M2.5 12s3.5-6 9.5-6 9.5 6 9.5 6-3.5 6-9.5 6-9.5-6-9.5-6Z"/><circle cx="12" cy="12" r="2.8"/></svg>',
  info: '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M12 10.5v6M12 7.5h.01"/></svg>',
  gear: '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2.83 2.83-.06-.06a1.7 1.7 0 0 0-1.88-.34 1.7 1.7 0 0 0-1.03 1.56V21h-4v-.08A1.7 1.7 0 0 0 9 19.37a1.7 1.7 0 0 0-1.88.34l-.06.06-2.83-2.83.06-.06A1.7 1.7 0 0 0 4.63 15a1.7 1.7 0 0 0-1.55-1H3v-4h.08A1.7 1.7 0 0 0 4.63 9a1.7 1.7 0 0 0-.34-1.88l-.06-.06 2.83-2.83.06.06A1.7 1.7 0 0 0 9 4.63a1.7 1.7 0 0 0 1-1.55V3h4v.08A1.7 1.7 0 0 0 15 4.63a1.7 1.7 0 0 0 1.88-.34l.06-.06 2.83 2.83-.06.06A1.7 1.7 0 0 0 19.37 9a1.7 1.7 0 0 0 1.55 1H21v4h-.08A1.7 1.7 0 0 0 19.4 15Z"/></svg>',
  chevron: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m7 10 5 5 5-5"/></svg>',
}[name]);

let duration = 30;
let account = 'Conta atual';
const brand = (large = false) => `<div class="brand ${large ? 'brand-large' : ''}"><img src="${brandMark}" alt=""><strong>Compasso</strong></div>`;

function addTimeView() {
  return `<main class="screen add-time">
    <header class="app-brand">${brand()}</header>
    <section class="content">
      <h1>Adicionar tempo</h1>
      <p class="lead">Escolha o período e confirme com a senha do responsável.</p>
      <div class="connection"><span></span> Servidor conectado</div>
      <div class="durations" role="radiogroup" aria-label="Período">
        ${[15, 30, 60, 120].map(value => `<button class="duration ${value === duration ? 'selected' : ''}" role="radio" aria-checked="${value === duration}" data-duration="${value}">${value} min</button>`).join('')}
      </div>
      <label class="field-label" for="parent-password">Senha do responsável</label>
      <div class="password-field"><input id="parent-password" type="password" placeholder="Digite sua senha" autocomplete="current-password"><button class="reveal" type="button" aria-label="Mostrar senha">${icon('eye')}</button></div>
      <button class="primary add-button" type="button">Adicionar ${duration} minutos</button>
      <p class="privacy">${icon('info')}<em>A senha <strong>não será</strong> armazenada.</em></p>
    </section>
    <footer><button class="link-button settings-link" type="button">${icon('gear')} Configurações</button></footer>
  </main>`;
}

function settingsView() {
  return `<main class="screen settings"><section class="settings-content">
    ${brand(true)}
    <h1>Configurações do Compasso</h1>
    <p class="settings-lead">Conecte este computador ao seu painel familiar.</p>
    <div class="status">Ainda não configurado</div>
    <form id="settings-form">
      <fieldset><legend>Conta que será controlada</legend>
        <label for="windows-account">Conta Windows</label>
        <div class="select-wrap"><select id="windows-account"><option>${account}</option></select>${icon('chevron')}</div>
        <label class="consent"><input type="checkbox" required checked><span>Confirmo que esta conta poderá ter a sessão bloqueada pelo Compasso.</span></label>
      </fieldset>
      <fieldset><legend>Conexão com o painel</legend>
        <label for="server">Endereço do servidor</label><input id="server" type="url" value="https://apifamily.smresume.com" required>
        <label for="device-id">Identificador do dispositivo</label><input id="device-id" placeholder="Cole o identificador gerado no painel" required>
        <label for="token">Token do dispositivo</label>
        <div class="token-field"><input id="token" type="password" required><button class="show-token" type="button">Mostrar</button></div>
        <p class="token-note">O token fica protegido neste computador.</p>
      </fieldset>
      <button class="primary save-button" type="submit">Salvar e conectar</button>
    </form>
    <button class="back-button" type="button">Voltar para adicionar tempo</button>
  </section></main>`;
}

function bindAddTime() {
  document.querySelectorAll('.duration').forEach(button => button.addEventListener('click', () => {
    duration = Number(button.dataset.duration);
    render('add');
  }));
  const password = document.querySelector('#parent-password');
  document.querySelector('.reveal').addEventListener('click', event => {
    const show = password.type === 'password';
    password.type = show ? 'text' : 'password';
    event.currentTarget.setAttribute('aria-label', show ? 'Ocultar senha' : 'Mostrar senha');
  });
  document.querySelector('.settings-link').addEventListener('click', () => render('settings'));
  document.querySelector('.add-button').addEventListener('click', () => {
    if (!password.value) { password.focus(); return; }
    document.querySelector('.add-button').textContent = `Solicitação de ${duration} minutos enviada`;
  });
}

function bindSettings() {
  const token = document.querySelector('#token');
  document.querySelector('.show-token').addEventListener('click', event => {
    const show = token.type === 'password';
    token.type = show ? 'text' : 'password';
    event.currentTarget.textContent = show ? 'Ocultar' : 'Mostrar';
  });
  document.querySelector('.back-button').addEventListener('click', () => render('add'));
  document.querySelector('#settings-form').addEventListener('submit', event => {
    event.preventDefault();
    document.querySelector('.save-button').textContent = 'Configuração salva';
    document.querySelector('.status').textContent = 'Configuração salva';
  });
}

function render(view = 'add') {
  document.querySelector('#app').innerHTML = view === 'settings' ? settingsView() : addTimeView();
  view === 'settings' ? bindSettings() : bindAddTime();
}

WindowsUser().then(value => { account = value || account; }).catch(() => {}).finally(() => render('add'));
