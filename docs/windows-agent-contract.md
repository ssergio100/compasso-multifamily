# Contrato para portar o agente para Windows

Este documento resume o comportamento que a versão Windows deve preservar. A
fonte executável continua sendo `agent/`, `protocol/v1/` e o processamento de
heartbeat em `server/web/api.go` e `server/storage/sync.go`.

## Escopo e arquitetura

- A interface vigente é `windows/CompassoWails`. `CompassoApp` e
  `CompassoInstaller` são históricos e não devem receber a integração.
- O instalador e as telas já existem, mas ainda são somente a camada visual.
- Um serviço Windows separado deve executar sincronização, política, contagem,
  persistência e controle da sessão mesmo quando a interface estiver fechada.
- A interface Wails deve conversar localmente com o serviço para consultar
  estado, adicionar tempo e configurar o agente. Senhas e token não devem ir
  para argumentos de processo nem para logs.
- Os pacotes Go independentes de sistema operacional (`policy`, `storage`,
  `syncclient`, `localauth`, `syncstatus` e `protocol/v1`) devem ser
  reaproveitados. Integrações Linux (`loginctl`, D-Bus, systemd, GTK e
  `notify-send`) devem ter adaptadores Windows, sem duplicar o motor.

## Responsabilidades do agente

O agente observa apenas a conta configurada, detecta sua sessão gráfica local,
mede tempo decorrido com relógio monotônico, avalia a política vigente, exibe
avisos e solicita o bloqueio sem encerrar aplicativos. Ele mantém estado local
durável, sincroniza com o servidor e expõe à interface somente operações
locais restritas.

A precedência da política é: monitoramento suspenso, bloqueio manual, rotina
ativa e saldo diário esgotado. O tempo só é contado quando a decisão permite
uso, existe sessão gráfica da conta controlada e ela está desbloqueada. **Uma
tela bloqueada jamais deve consumir ou decrementar tempo.** A contagem deve
parar assim que o bloqueio for observado e só recomeçar após o desbloqueio
manual.

## Comunicação com o servidor

O agente sempre inicia a comunicação com `POST
/api/v1/device/heartbeat`. Não existe conexão iniciada pelo servidor.

Cabeçalhos obrigatórios:

- `Authorization: Bearer <device_token>`;
- `X-Tempo-Device-ID: <device_id>`;
- `X-Compasso-Protocol-Version: 2`;
- `X-Compasso-Agent-Installation-ID: <UUID v4>`;
- `X-Compasso-Capabilities: next-heartbeat-seconds,
  command-ack-receipts, installation-identity`.

O heartbeat envia revisão de política e controle, revisão da âncora de sessão,
data local, segundos consumidos, presença/identidade/bloqueio da sessão,
pedido de nova âncora, eventos locais pendentes e confirmações de comandos.

O servidor responde com horário, próximo intervalo, IDs de eventos e comandos
recebidos, política completa quando necessário, âncora autoritativa do saldo,
controle remoto online e comandos pendentes. O intervalo normal é 5 segundos
com sessão gráfica e 30 segundos sem sessão; o agente aceita somente valores
entre 1 segundo e 10 minutos e acrescenta jitter de até 10%.

## Comandos recebidos e confirmações

| Comando | Efeito local | Quando confirmar |
| --- | --- | --- |
| `add_bonus` | Somar crédito ao dia local informado pelo próprio heartbeat | Depois de persistir bônus e ID do comando atomicamente |
| `block_now` | Bloquear a sessão controlada | Depois de observar a sessão bloqueada |
| `clear_manual_block` | Parar de impor o bloqueio manual | Depois de observar sessão ausente ou desbloqueada manualmente |
| `pause_monitoring` | Suspender bloqueios e contagem | Depois de observar sessão ausente ou desbloqueada manualmente |
| `resume_monitoring` | Retomar política e contagem | Depois de persistir/aplicar o novo estado |

Comandos são idempotentes. Seus IDs ficam persistidos e são reenviados em
`command_acks` até o servidor devolvê-los em `acknowledged_commands`. Controles
remotos são autoridade somente enquanto o último heartbeat foi bem-sucedido;
ao perder comunicação, `monitoring_paused` e `manual_block` remotos são
descartados e a última política durável volta a reger o computador.

A operação local de adicionar tempo gera um evento `bonus_added`, com UUID,
data local, segundos e origem `local`. O evento fica na fila até aparecer em
`acknowledged_events`. A senha é verificada no serviço contra o verificador
Argon2id da política e nunca é armazenada. São aceitos de 1 minuto a 12 horas;
a interface vigente oferece 15, 30, 60 e 120 minutos. Falhas de senha usam
esperas progressivas de 2 s, 5 s, 15 s, 1 min e 5 min.

## Estados e persistência

O SQLite local deve continuar contendo:

- política e rotinas por revisão;
- uso diário monotônico, salvo a cada 5 segundos e no encerramento normal;
- bônus locais e remotos;
- fila de eventos locais;
- comandos aplicados ainda não confirmados pelo servidor;
- efeito de controle pendente;
- âncora de saldo confirmada para sessão/data/revisão;
- vínculo de enrollment, impressão do token e UUID da instalação.

Trocar servidor ou dispositivo limpa o estado do enrollment anterior. Trocar o
token gera nova identidade de instalação. Atualizar mantendo as mesmas
credenciais preserva a identidade e o estado válido. A configuração e o banco
devem ficar sob `%ProgramData%\Compasso`, acessíveis somente a SYSTEM e
administradores; o token deve ser protegido pelo Windows e nunca ser devolvido
à interface depois de salvo.

## Inicialização e perda de comunicação

O serviço deve iniciar automaticamente com o Windows. Sem configuração
completa, ou antes da confirmação do primeiro heartbeat durante a configuração,
ele permanece ativo mas não aplica bloqueios. Ao salvar uma configuração, deve
validar conta, URL e credenciais, iniciar/reiniciar a sincronização e só marcar
o setup como concluído depois de uma resposta válida do servidor.

Falhas de rede e `5xx` usam backoff exponencial até 5 minutos. `429` respeita
`Retry-After` entre 5 segundos e 1 hora. Credenciais inválidas, família
suspensa, conflito de instalação e revisão incompatível esperam 6 horas;
demais `4xx` esperam 1 hora. O serviço continua usando política, uso, bônus e
filas locais duráveis. Nenhuma falha confirma evento ou comando.

## Sessão e diferenças inevitáveis no Windows

- `loginctl` deve ser substituído pelas APIs de sessão do Windows, usando a
  identidade persistente da conta (SID), e sessões remotas não devem contar.
- A regra de não consumir tempo durante bloqueio também pertence ao motor
  compartilhado: qualquer plataforma deve pausar a contagem durante todo o
  período em que a sessão estiver bloqueada.
- O bloqueio deve preservar aplicativos e ser confirmado pelo estado real da
  sessão; não se deve encerrar o usuário como fallback.
- O Windows não permite ao Compasso desbloquear remotamente a sessão. Ao
  receber `clear_manual_block` ou `pause_monitoring` com a máquina bloqueada, o
  serviço deixa de impor o bloqueio e aguarda o desbloqueio manual antes de
  confirmar o efeito.
- D-Bus vira IPC local do Windows com ACL explícita; systemd vira Windows
  Service; notificações freedesktop viram notificações da sessão controlada.
- O serviço não pode depender do processo Wails nem de um usuário conectado.

## IPC local no Windows

O serviço expõe as operações privilegiadas em `\\.\pipe\CompassoAgent`, o
equivalente do D-Bus de sistema do Linux. O transporte e seu controle de acesso
ficam em `agent/windowsipc`; o motor de bônus, a verificação de senha e os textos
permanecem nos pacotes compartilhados.

- Enquadramento: prefixo `uint32` little-endian com o tamanho do payload,
  seguido de JSON. Payload limitado a 64 KiB; tamanho fora da faixa é violação
  de protocolo, nunca uma alocação.
- Operações: `add_local_bonus`, `get_synchronization_report`, `ping` e
  `get_public_configuration`.
- O serviço responde uma requisição por conexão e fecha a conexão em seguida,
  para não deixar contexto autenticado aberto no pipe.
- O token do dispositivo nunca atravessa o pipe; a operação de configuração
  expõe somente dados publicáveis.

Controle de acesso, medido nesta plataforma:

- O DACL concede controle total a LocalSystem e ao grupo de administradores, e
  apenas leitura e escrita (`GRGW`) à conta controlada. `GRGW` é suficiente
  porque a interface só envia uma requisição e lê a resposta; `GA` incluiria
  `FILE_CREATE_PIPE_INSTANCE` e permitiria que um processo da conta controlada
  criasse instâncias do pipe.
- A primeira instância usa `FILE_FLAG_FIRST_PIPE_INSTANCE`, que faz a criação
  falhar quando o nome já existe. Sem isso, um processo da conta controlada
  poderia sequestrar o nome do pipe, receber a senha do responsável e forjar uma
  resposta de bônus concedido.
- `PIPE_REJECT_REMOTE_CLIENTS` **não é usado**: este build do Windows rejeita a
  flag com `ERROR_INVALID_PARAMETER`, verificado em Go e nativo, em todas as
  combinações de descritor de segurança e de contagem de instâncias. Clientes
  remotos são negados pelo DACL, porque um token autenticado pela rede nunca é
  LocalSystem, administrador ou a conta controlada.

Se uma versão futura do Windows aceitar a flag, ela deve ser somada como
defesa adicional, nunca em substituição ao DACL.

O pipe é reconstruído automaticamente se a interface parar, sem reiniciar o
serviço. A aplicação de política e o bloqueio de sessão continuam ativos enquanto
a interface está indisponível: o serviço nunca deixa de fazer valer a política
porque um canal de interface falhou.

## Ligação das interfaces

### Adicionar tempo

- Consultar serviço e estado de sincronização (`checking`, `online`,
  `offline`, indisponível) periodicamente.
- Enviar senha e duração selecionada ao serviço.
- Exibir sucesso somente depois do bônus e do evento terem sido persistidos.
- Mapear senha ausente, senha não configurada, senha inválida, limite de
  tentativas, serviço indisponível e falha interna para mensagens humanas.
- Limpar a senha após a tentativa e nunca gravá-la ou registrá-la.
- Abrir a tela de configurações já existente.

### Configurações

- Listar contas locais elegíveis e persistir o SID selecionado.
- Exigir confirmação explícita da conta que poderá ser bloqueada.
- Validar origem HTTP(S), permitindo HTTP apenas para loopback, além de
  `device_id` e token não vazios.
- Salvar por caminho privilegiado, sem expor o token na linha de comando.
- Iniciar/reiniciar o serviço, aguardar até 30 segundos pela primeira resposta
  válida e mostrar o diagnóstico sanitizado.
- Exibir token vazio em reaberturas; valor protegido não deve ser recuperado.

### Serviço/background

Devem permanecer fora da interface: SQLite e migrações, credenciais,
heartbeat/retry, relógio monotônico, avaliação da política, detecção e bloqueio
de sessão, alertas, filas idempotentes, acknowledgements e logs técnicos.
