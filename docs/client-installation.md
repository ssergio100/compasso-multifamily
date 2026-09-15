# Compasso Client — instalação e operação

## Instalação

Instale o pacote na máquina que será controlada:

```bash
sudo apt install ./compasso-client_<versão>_amd64.deb
```

Uma instalação nova mantém `tempo-agent.service` desabilitado. Abra
**Compasso**, clique na engrenagem e informe a conta Linux controlada, o endereço
HTTPS do servidor, `device_id` e `device_token`. No pacote oficial, o assistente
já sugere `https://apifamily.smresume.com`; o endereço só é gravado junto das
duas credenciais. O assistente só conclui depois que o servidor aceitar o
primeiro heartbeat.

## Uso

O aplicativo **Compasso** abre a concessão local de tempo. A engrenagem no canto
inferior direito abre novamente a configuração de servidor e credenciais.

A própria janela mostra o estado da comunicação. Quando houver falha, ela
explica em linguagem direta se o servidor recusou a sincronização, se a
credencial precisa ser revista, se o agente exige atualização ou se houve uma
indisponibilidade de rede. A mensagem técnica bruta e o token nunca são
exibidos.

O agente roda como serviço systemd e continua aplicando a última autorização
válida durante interrupções de rede:

```bash
systemctl status tempo-agent.service
journalctl -u tempo-agent.service --no-pager -n 50
```

O tempo é contabilizado somente enquanto a conta controlada possui uma sessão
gráfica local no estado `active` e destravada. Encerrar a sessão ou travar a
tela interrompe a contagem na próxima observação do agente, e o heartbeat
seguinte atualiza o painel.

## Atualização e remoção

Atualizações preservam configuração, banco local e estado do serviço:

```bash
sudo apt install ./compasso-client_<nova-versão>_amd64.deb
```

Remover o pacote mantém configuração e estado. Para apagá-los também, use:

```bash
sudo apt purge compasso-client
```
