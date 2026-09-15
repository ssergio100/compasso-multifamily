# Especificação vigente de comunicação — ADM, servidor e agente

## 1. Finalidade e condição deste documento

Este documento descreve o comportamento **atualmente implementado** na
comunicação entre os três componentes distribuídos do Compasso:

- a interface administrativa web, chamada neste documento de **ADM**;
- a API e o banco centrais, chamados de **servidor**;
- o serviço privilegiado instalado no computador controlado, chamado de
  **agente**.

Ele deve ser usado como:

- especificação para reproduzir a arquitetura em outra implementação;
- referência para revisar mudanças de protocolo;
- base para testes de integração e aceitação;
- mapa para investigar falhas de comunicação;
- registro das limitações atuais e ponto de partida para melhorias.

Estado observado: 24 de agosto de 2026.

Versões estruturais vigentes:

- protocolo agente–servidor: `2`;
- endpoint de sincronização: `/api/v1/device/heartbeat`;
- esquema SQLite do agente: migração `4`;
- esquema SQLite do servidor: migração `10`;
- única interface ADM: `admin-ui`.

Este documento é canônico para a arquitetura, mas os tipos em
`protocol/v1/sync.go`, as rotas em `server/web` e as transações em
`server/storage` continuam sendo a fonte executável do contrato. Toda mudança
nesses arquivos que altere comportamento distribuído deve atualizar este
documento no mesmo commit.

As palavras **DEVE**, **NÃO DEVE**, **PODE** e **RECOMENDA-SE** indicam regras
normativas. Se uma descrição estiver marcada como “limitação atual”, ela
documenta o sistema existente e não necessariamente o comportamento ideal.

## 2. Visão geral

### 2.1 Topologia

```text
Pessoa responsável
        │
        ▼
ADM no navegador
        │ HTTP JSON autenticado por sessão + CSRF
        ▼
Servidor/API ───────────────► SQLite central
        ▲
        │ HTTPS JSON autenticado por device_id + token
        │ heartbeat iniciado sempre pelo agente
        ▼
Agente privilegiado ────────► SQLite local
        │
        ├── D-Bus de sistema ◄── interface local sem privilégios
        └── logind / sessão gráfica controlada

Servidor ── SSE autenticado por sessão ──► ADM
```

### 2.2 Invariantes arquiteturais

1. O ADM **nunca conversa diretamente com o agente**.
2. O agente **nunca recebe conexão iniciada pelo servidor**. Ele inicia todos
   os heartbeats.
3. Ações do ADM chegam primeiro ao servidor e são persistidas antes de qualquer
   tentativa de entrega ao agente.
4. O servidor entrega política, controle, saldo e comandos somente na resposta
   de um heartbeat.
5. O agente continua aplicando o estado local quando o servidor está
   indisponível, exceto o controle remoto explicitamente classificado como
   `online-only`.
6. O ADM recebe atualizações do servidor por SSE, mas reconstrói o estado
   durável por HTTP após abertura ou reconexão do stream.
7. Resposta HTTP `202` a uma ação assíncrona significa “pedido persistido”, não
   “ação aplicada no computador”.
8. Para comandos, sucesso no computador exige `command_ack` posterior.
9. Reenvio da mesma operação não pode duplicar seu efeito.
10. Senhas, tokens, cookies e cabeçalhos de autorização não podem aparecer em
    atividades, diagnóstico técnico ou mensagens de erro.

## 3. Responsabilidades e autoridades

### 3.1 ADM

O ADM é responsável por:

- autenticar a pessoa administradora;
- apresentar dispositivos, estado, limites, rotinas e ações;
- enviar intenções administrativas por HTTP JSON;
- guardar temporariamente os `operation_id` iniciados na aba aberta;
- manter um único `EventSource` para o dispositivo selecionado;
- mostrar estado vivo, atividades humanas e diagnóstico técnico;
- reconciliar o estado persistido quando o stream abre ou sinaliza mudança;
- explicar espera, sucesso e erro em linguagem humana.

O ADM não é autoridade sobre saldo, política aplicada, entrega ou conclusão.
Atualizações otimistas podem melhorar a resposta visual, mas o servidor deve
substituí-las no próximo carregamento ou evento.

### 3.2 Servidor

O servidor é responsável por:

- autenticar administradores e agentes;
- validar todos os payloads e limites;
- persistir configuração desejada, uso consolidado, bônus, comandos e eventos;
- atribuir identificadores opacos às operações;
- controlar revisões de política, controle e âncora de sessão;
- responder ao heartbeat com o estado necessário;
- registrar ofertas de comandos e confirmações do agente;
- calcular a visão do estado vivo exibida no ADM;
- manter histórico humano, auditoria e diagnóstico técnico separados;
- publicar eventos SSE para navegadores inscritos;
- detectar dispositivo offline a partir do último heartbeat.

O servidor é a autoridade sobre a configuração desejada e sobre o histórico
central. Ele não é a autoridade que aplica bloqueio no sistema operacional.

### 3.3 Agente

O agente é responsável por:

- observar a sessão gráfica da conta controlada;
- contar uso e persistir checkpoints;
- avaliar e aplicar localmente a última política válida;
- funcionar offline com o estado durável local;
- enviar heartbeat, uso, revisões, eventos e confirmações;
- validar e persistir respostas antes de reconhecê-las;
- aplicar comandos de modo idempotente;
- manter confirmações de comandos para reenvio;
- informar o último erro sanitizado à interface local por D-Bus;
- nunca expor o token do dispositivo em logs ou mensagens.

O agente é a autoridade final sobre o estado efetivamente aplicado no
computador.

## 4. Endereçamento, configuração e tempos

### 4.1 Implantação padrão

| Componente | Endereço padrão de implantação |
| --- | --- |
| ADM | `http://<servidor>:8182` |
| API | `http://<servidor>:8181` na LAN ou origem HTTPS pública |
| Heartbeat | `<server_url>/api/v1/device/heartbeat` |
| SSE | `<api_base>/api/v1/admin/devices/{device_id}/stream` |

Em produção remota, o agente aceita `server_url` HTTP apenas para loopback.
Servidor em outra máquina **DEVE** usar HTTPS.

### 4.2 Tempos padrão

| Regra | Valor padrão | Limites implementados |
| --- | ---: | ---: |
| Heartbeat normal orientado pelo servidor | 3 s | 1 s a 10 min |
| Timeout de uma tentativa HTTP do agente | 8 s | 1 s a 1 min |
| Dispositivo considerado offline | 60 s | 10 s a 10 min |
| Keep-alive SSE | 15 s | constante atual |
| Sessão administrativa | 8 h | 1 min a 7 dias |
| Registro repetido de falha do agente | no máximo 1/min | constante atual |
| Retenção da atividade concluída | 30 dias | fixa atualmente |
| Retenção padrão do diagnóstico | 30 dias | configurável de 1 a 365 dias pela API |

O seletor atual do ADM oferece 1, 7, 15, 30, 60 e 90 dias para o diagnóstico,
embora a API aceite até 365 dias.

O servidor define um único intervalo global em `heartbeat_interval` ou em
`TEMPO_HEARTBEAT_INTERVAL`. O agente usa 3 segundos embutidos antes da primeira
resposta válida e sempre que o campo remoto estiver ausente ou fora dos limites.
O `heartbeat_interval` local continua sendo lido para compatibilidade com
instalações existentes, mas não controla mais o processo instalado.

### 4.3 Retry do agente

Após uma falha, o agente:

1. marca a comunicação como offline;
2. descarta o controle remoto `online-only` recebido anteriormente;
3. inicia backoff em 1 segundo, ou no intervalo normal se ele for menor;
4. dobra o backoff após cada falha;
5. limita o backoff ao intervalo normal do heartbeat;
6. desconta do atraso o tempo já gasto na tentativa;
7. volta ao intervalo normal depois do primeiro sucesso.

Quando uma resposta válida contém comandos, o agente faz um heartbeat adicional
imediatamente após persistir sua aplicação. Esse ciclo acelerado transporta os
`command_acks` sem aguardar o intervalo normal. Apenas um ciclo consecutivo pode
ser acelerado, evitando um loop ocupado se o servidor repetir um comando.

A primeira falha, a transição online → offline e a recuperação são registradas
imediatamente. Enquanto continua offline, o agente registra novo resumo no
máximo uma vez por minuto.

## 5. Identidade, autenticação e segurança

### 5.1 Administrador

O ADM usa uma sessão mantida em memória pelo processo servidor.

- cookie: `tempo_admin_session`;
- `HttpOnly`: obrigatório;
- `SameSite=Strict`;
- escopo: `/api/v1/admin`;
- `Secure`: controlado por `secure_cookies`, obrigatório em produção HTTPS;
- expiração: `session_lifetime` sem renovação deslizante.

Uma reinicialização do servidor elimina as sessões em memória e exige novo
login.

Antes do login ou da configuração inicial, o servidor cria o cookie temporário
`tempo_login_csrf`, válido por 10 minutos. Login e setup enviam o mesmo valor
também no JSON. Operações mutáveis autenticadas enviam o CSRF da sessão no
cabeçalho `X-CSRF-Token`.

### 5.2 CORS do ADM

O servidor aceita credenciais do navegador apenas para `admin_origin`.

- uma URL absoluta libera somente aquela origem;
- `same-host` aceita origem cujo hostname seja igual ao hostname da API,
  permitindo portas diferentes;
- origem não autorizada recebe `403`;
- preflight permite `GET, POST, PUT, PATCH, DELETE, OPTIONS`;
- cabeçalhos permitidos: `Content-Type` e `X-CSRF-Token`;
- preflight pode ser armazenado por 600 segundos.

### 5.3 Agente

Cada heartbeat deve conter:

```http
Authorization: Bearer <device_token>
X-Tempo-Device-ID: <device_id>
X-Compasso-Protocol-Version: 2
Content-Type: application/json
```

O token possui 256 bits aleatórios, codificados em Base64 URL sem padding. O
servidor mostra o segredo somente na emissão e persiste apenas seu SHA-256. A
comparação do hash é feita em tempo constante.

Revogar o token torna o heartbeat seguinte inválido imediatamente. Emitir novo
token substitui o anterior.

## 6. API administrativa atual

Todas as rotas abaixo, exceto leitura/criação inicial de sessão e setup,
exigem sessão administrativa. Toda mutação exige CSRF.

| Método e rota | Função | Resultado principal |
| --- | --- | --- |
| `GET /api/v1/admin/session` | inspecionar sessão/setup | sessão e CSRF |
| `POST /api/v1/admin/session` | login | sessão autenticada |
| `DELETE /api/v1/admin/session` | logout | `204` |
| `POST /api/v1/admin/setup` | criar primeiro administrador | sessão criada |
| `GET /api/v1/admin/devices` | listar dispositivos | resumos |
| `POST /api/v1/admin/devices` | cadastrar dispositivo e escolher avatar | `201` + dispositivo |
| `GET /api/v1/admin/devices/{id}` | detalhe completo | dispositivo, política, controle, estado e auditoria |
| `PATCH /api/v1/admin/devices/{id}` | alterar nome e avatar | `200` |
| `DELETE /api/v1/admin/devices/{id}` | excluir e apagar dados relacionados | `204` |
| `GET /api/v1/admin/devices/{id}/status` | estado calculado | snapshot vivo |
| `PUT /api/v1/admin/devices/{id}/policy` | cotas semanais e aviso | `200` |
| `POST /api/v1/admin/devices/{id}/routines` | criar rotina e escolher ícone | `201` + ID do servidor |
| `PUT /api/v1/admin/devices/{id}/routines/{routine_id}` | alterar rotina e ícone | `200` |
| `DELETE /api/v1/admin/devices/{id}/routines/{routine_id}` | excluir rotina | `204` |
| `PUT /api/v1/admin/devices/{id}/password` | trocar senha local | `200` |
| `POST /api/v1/admin/devices/{id}/token` | emitir/substituir token | `201`, segredo uma vez |
| `DELETE /api/v1/admin/devices/{id}/token` | revogar token | `204` |
| `POST /api/v1/admin/devices/{id}/bonus` | enfileirar bônus remoto | `202` + `operation_id` |
| `POST /api/v1/admin/devices/{id}/commands` | enfileirar controle | `202` + `operation_id` |
| `GET /api/v1/admin/devices/{id}/commands/{operation_id}` | compatibilidade: consultar ack de bônus | `{acknowledged}` |
| `GET /api/v1/admin/devices/{id}/activities` | listar histórico humano | até 200 itens |
| `GET /api/v1/admin/devices/{id}/activities/{activity_id}` | carregar uma atividade | atividade e etapas |
| `DELETE /api/v1/admin/devices/{id}/activities/completed` | ocultar concluídas | quantidade ocultada |
| `GET /api/v1/admin/devices/{id}/events` | auditoria bruta | até 200 itens |
| `GET /api/v1/admin/devices/{id}/communication` | diagnóstico técnico | paginação incremental |
| `DELETE /api/v1/admin/devices/{id}/communication` | apagar diagnóstico | quantidade apagada |
| `PUT /api/v1/admin/devices/{id}/communication/settings` | retenção técnica global | dias configurados |
| `GET /api/v1/admin/devices/{id}/stream` | abrir SSE | stream |

Corpos JSON administrativos são limitados a 64 KiB, rejeitam campos
desconhecidos e não aceitam conteúdo extra depois do primeiro objeto.

As identidades visuais são persistidas como chaves lógicas, nunca como caminhos
de arquivos. O campo `avatar_key` identifica o avatar do dispositivo e
`icon_key` identifica o ícone da rotina. Clientes antigos continuam aceitos:
na criação, a omissão aplica o padrão; na edição, preserva o valor já salvo.
Esses campos pertencem à administração e não fazem parte do heartbeat do agente.

### 6.1 Validações de negócio relevantes

| Objeto | Regra atual |
| --- | --- |
| Login inicial | texto não vazio, até 80 caracteres |
| Senha administrativa inicial | não vazia, confirmação igual, até 4096 caracteres |
| Nome do dispositivo | texto não vazio depois de remover espaços externos, até 80 caracteres |
| Avatar do dispositivo | coleção vigente: `capybara`, `cat`, `chick`, `dog`, `fox`, `lion`, `owl`, `panda`, `penguin`, `rabbit`, `sheep` ou `tiger`; chaves antigas continuam aceitas para compatibilidade |
| Cota de cada dia | 0 a 86.400 segundos |
| Aviso | 0 a 120 minutos no servidor; o ADM oferece valores predefinidos |
| Nome da rotina | texto não vazio, até 80 caracteres |
| Ícone da rotina | `study`, `reading`, `sleep`, `bath`, `meal`, `school`, `exercise`, `chores`, `family`, `music`, `outdoor` ou `general` |
| Horário da rotina | início e fim entre 0 e 86.399 segundos |
| Dias da rotina | pelo menos um dos sete dias |
| Conflito de rotina | nenhum intervalo efetivo pode sobrepor outro no mesmo dispositivo |
| Bônus pelo ADM | 1 a 720 minutos |
| Comando de controle | somente `pause_monitoring`, `resume_monitoring`, `block_now` ou `clear_manual_block` |
| Retenção técnica | 1 a 365 dias |
| Limite de atividades | 1 a 200; padrão 100 |
| Limite do diagnóstico | 1 a 500; ADM usa 200 |

Rotina com início igual ao fim representa o dia inteiro. Rotina com início
maior que o fim atravessa a meia-noite e ocupa também o começo do dia seguinte.
O teste de conflito considera esses intervalos semanais expandidos; o servidor,
e não apenas o ADM, é responsável por rejeitar sobreposição com `409`.

## 7. Contrato do heartbeat

### 7.1 Requisição

O heartbeat é sempre `POST /api/v1/device/heartbeat`. O corpo máximo é 256 KiB
e rejeita campos desconhecidos ou JSON adicional.

```json
{
  "policy_revision": 12,
  "control_revision": 7,
  "session_state_revision": 12,
  "local_date": "2026-08-24",
  "seconds_used": 3600,
  "graphical_session_active": true,
  "graphical_session_id": "session-id",
  "graphical_session_locked": false,
  "request_session_state": false,
  "events": [],
  "command_acks": []
}
```

Regras:

- revisões e contadores não podem ser negativos;
- `seconds_used` não pode exceder 48 horas;
- `local_date` deve ser uma data ISO real do computador;
- sessão ativa exige identificador opaco válido;
- sessão inativa não pode informar ID, bloqueio nem solicitar âncora;
- no máximo 100 eventos e 100 confirmações por heartbeat;
- identificadores opacos aceitam letras, números, `-` e `_`, com até 128
  caracteres.

### 7.2 Resposta

```json
{
  "server_time": "2026-08-24T12:00:00Z",
  "next_heartbeat_seconds": 3,
  "acknowledged_events": [],
  "policy": null,
  "session_state": null,
  "commands": [],
  "control": {
    "revision": 7,
    "monitoring_paused": false,
    "manual_block": false
  }
}
```

O servidor sempre envia `control`. Os demais campos são condicionais:

- `next_heartbeat_seconds`: intervalo global do próximo ciclo normal, enviado
  apenas quando o agente anuncia a capacidade `next-heartbeat-seconds`;
- `acknowledged_events`: eventos locais aceitos nesta requisição;
- `policy`: política completa quando a revisão do agente está atrasada;
- `session_state`: âncora de saldo quando solicitada ou desatualizada;
- `commands`: até 100 comandos ainda não reconhecidos; bônus são cumulativos,
  enquanto existe no máximo um comando de controle pendente por dispositivo.

### 7.3 Compatibilidade

Ausência do cabeçalho de versão é interpretada como protocolo `1`.

- versões diferentes de `1` e `2` recebem `400`;
- o agente capaz envia
  `X-Compasso-Capabilities: next-heartbeat-seconds`;
- sem esse anúncio, o servidor omite o novo campo para não quebrar agentes
  antigos que rejeitam propriedades desconhecidas;
- servidores antigos ignoram o cabeçalho, e o agente novo usa o valor seguro
  embutido quando a resposta não contém o campo;
- agente v1 pode sincronizar enquanto não existir bônus remoto pendente;
- agente v1 com bônus remoto pendente recebe `426` e código
  `agent_upgrade_required`;
- revisão local maior que a do servidor recebe `409`, código
  `revision_ahead` e as duas revisões.

O erro `revision_ahead` normalmente significa cadastro local pertencente a
outro dispositivo ou restauração do servidor a partir de backup antigo. O
servidor não deve sobrescrever silenciosamente o estado local nesse caso.

## 8. Revisões e estados sincronizados

### 8.1 Política

A política contém:

- revisão;
- aviso em minutos;
- verificador Argon2id da senha local;
- sete cotas em segundos;
- rotinas com ID, nome, dias, início, fim e estado habilitado.

Alterar cota, aviso, rotina, senha local ou bônus remoto incrementa a revisão
de política no servidor. Quando `agent.policy_revision < server.revision`, o
servidor devolve a política completa. O agente substitui a política local
transacionalmente e passa a informar a nova revisão no próximo heartbeat.

Se o agente informar revisão de política maior que a do servidor, todo o
heartbeat é rejeitado com `409`.

### 8.2 Controle remoto

Pausa e bloqueio manual usam `device_control`, com revisão própria:

- `pause_monitoring`: pausa, remove bloqueio manual, desbloqueia a sessão e
  libera sem contar tempo;
- `resume_monitoring`: retorna ao estado ativo;
- `block_now`: remove a pausa e ativa bloqueio manual;
- `clear_manual_block`: retorna ao estado ativo e desbloqueia a sessão quando
  a política local também permite acesso.

O servidor atualiza o estado desejado e incrementa `control.revision` na mesma
transação que cria o comando. A resposta de todo heartbeat leva o snapshot
atual de controle. Pausa e bloqueio são mutuamente exclusivos; o controle
representa sempre um entre os estados ativo, pausado ou bloqueado.

O controle é `online-only`: depois de falha de heartbeat, o cliente marca o
controle remoto como indisponível e não conserva o snapshot remoto apenas em
memória como prova de autoridade online. A política local durável continua
operando.

Conectividade e sessão gráfica são dimensões independentes. Quando a sessão
do usuário termina, o serviço do agente continua online e
trocando heartbeats, mas informa `graphical_session_active=false`. Um bloqueio
remoto permanece pendente até existir uma sessão que possa ser observada como
bloqueada.

### 8.3 Âncora de sessão e saldo

Quando há sessão gráfica ativa, o agente solicita uma âncora se:

- ainda não possui uma;
- mudou a sessão gráfica;
- mudou a data local;
- a revisão confirmada é menor que a revisão local de política.

O servidor calcula:

```text
remaining = cota do dia + bônus confirmados - uso consolidado
remaining = max(remaining, 0)
```

A âncora contém revisão, ID da sessão, data local, saldo restante, uso base e
horário de confirmação. O agente a aplica uma vez e desconta apenas uso
monotônico posterior. Heartbeat sem nova revisão não deve restaurar saldo já
consumido.

Se o agente pediu âncora e o servidor não a enviar, a tentativa falha. Se a
resposta trouxer política nova para sessão ativa, a âncora não pode ser mais
antiga nem ficar ausente.

### 8.4 Uso diário

O servidor consolida `seconds_used` com `MAX(valor_existente, valor_recebido)`.
Assim, repetição ou resposta perdida não reduz o uso central. A data usada é a
data local informada pelo agente, não a data civil do servidor.

## 9. Fluxos de ação

### 9.1 Matriz atual

| Ação | Registro autoritativo | Vai ao agente | Prova exibida como conclusão |
| --- | --- | --- | --- |
| Criar/renomear dispositivo | servidor | não | transação do servidor |
| Emitir/revogar token | servidor | afeta próxima autenticação | transação do servidor |
| Alterar cotas/aviso | política + revisão | política no heartbeat | **atualmente transação do servidor** |
| Criar/alterar/excluir rotina | política + revisão | política no heartbeat | **atualmente transação do servidor** |
| Alterar senha local | política + revisão | política no heartbeat | **atualmente transação do servidor** |
| Pausar/retomar | controle + comando | heartbeat | controle aplicado e efeito gráfico confirmado |
| Bloquear/desbloquear | controle + comando | heartbeat | `LockedHint` confirmou o estado solicitado |
| Bônus remoto | comando + revisão + bônus | heartbeat | `command_ack` |
| Bônus local | evento durável do agente | heartbeat | aceite em `acknowledged_events` |

“Transação do servidor” significa que o histórico humano diz explicitamente
que o servidor concluiu e registrou. Não significa que o agente já informou a
nova revisão aplicada.

### 9.2 Comando administrativo confirmado pelo agente

Aplica-se a bônus remoto, pausa, retomada, bloqueio e desbloqueio.

```text
ADM                 Servidor                 Agente
 │ POST ação           │                        │
 ├────────────────────►│                        │
 │                     │ grava comando +        │
 │                     │ atividade na mesma tx  │
 │◄── 202 + operation_id                        │
 │                     │                        │
 │                     │◄──── heartbeat ────────┤
 │                     │ conta oferta e inclui  │
 │                     │ o comando na resposta  │
 │                     ├──── comando ──────────►│
 │◄── activity_updated: offered                 │
 │                     │                        │ executa e observa efeito
 │                     │◄─ heartbeat + ack ─────┤
 │                     │ marca concluído        │
 │◄── activity_updated: completed               │
```

Regras:

1. O `operation_id` é também o ID do comando e da atividade humana.
2. A atividade nasce como `waiting_device` com etapas `requested/admin` e
   `stored/server`.
3. Cada vez que o servidor seleciona o comando para uma resposta, incrementa
   `delivery_attempts`, registra primeira/última oferta e agrega a etapa
   `offered/server`.
4. A contagem ocorre antes de o corpo HTTP chegar ao agente. Portanto a frase
   correta é “o servidor incluiu/ofereceu”, não “o computador recebeu”.
5. Para controles, o agente mantém a operação pendente até observar o efeito:
   bloqueado para `block_now`, desbloqueado para `clear_manual_block` e
   `pause_monitoring`. Sem sessão gráfica, o bloqueio aguarda uma sessão.
6. Somente depois do efeito o ID entra em `applied_command` e é reenviado no
   heartbeat, tornando perda de resposta inofensiva.
7. O servidor usa `COALESCE(acknowledged_at, agora)`: confirmações repetidas
   não alteram a primeira conclusão.
8. Confirmação histórica sem comando correspondente é aceita e devolvida ao
   agente quando ele anuncia a capacidade `command-ack-receipts`. Isso permite
   ao agente remover IDs já recebidos pelo servidor sem quebrar clientes
   antigos.
9. A atividade muda para `completed`, ganha etapa `completed/device` e expira
   30 dias depois.
10. Depois do ack, o envelope de um controle é removido de `device_command`;
    sua atividade temporária continua disponível. O comando de bônus permanece
    durável porque representa uma operação cumulativa.

### 9.3 Bônus remoto

O endpoint administrativo aceita de 1 minuto a 12 horas. O storage aceita até
24 horas para manter compatibilidade interna, mas a API é mais restritiva.

Ao enfileirar:

- o servidor gera UUID;
- cria `device_command(kind=add_bonus)`;
- grava payload com UUID, segundos, origem `web` e horário;
- incrementa a revisão de política;
- cria atividade humana pendente;
- responde `202`.

No primeiro heartbeat válido do computador:

1. o servidor associa o crédito pendente à `local_date` informada pelo agente;
2. `INSERT OR IGNORE` impede duplicação;
3. o comando é oferecido ao agente;
4. o agente aplica o bônus e o ID do comando em uma transação local;
5. o ID aparece em `command_acks` no heartbeat seguinte;
6. somente então o ADM apresenta confirmação do computador.

Enquanto não houver ack, o servidor subtrai o bônus remoto não reconhecido do
snapshot exibido no ADM. Isso impede que persistência central seja confundida
com aplicação confirmada.

### 9.4 Controles remotos

Comandos de controle não carregam payload de negócio: o estado autoritativo é
o snapshot `control` da mesma resposta. O comando fornece correlação e prova
idempotente de aplicação.

Controle não é uma fila de tarefas. Uma solicitação idêntica reutiliza a
operação ainda pendente; uma solicitação diferente substitui o comando e a
atividade pendentes anteriores. Assim, apenas a intenção mais recente será
oferecida quando o computador sincronizar. `add_bonus` não participa dessa
compactação, pois vários bônus devem ser somados.

O agente primeiro persiste o comando recebido como efeito pendente. Para
`block_now`, o ID só se torna reconhecível depois de `logind` informar a sessão
gráfica bloqueada. Para `clear_manual_block` e `pause_monitoring`, isso ocorre
depois de a sessão informar desbloqueio (ou imediatamente se já não existir
sessão gráfica). O ack segue em um heartbeat posterior; até lá, a interface
continua mostrando a transição correspondente.

### 9.5 Alterações de política

Cotas, aviso, rotinas e senha:

1. são validados pelo servidor;
2. são gravados em transação;
3. incrementam `policy.revision` e `device.policy_revision`;
4. geram auditoria;
5. geram atividade humana `completed` com atores `admin` e `server`;
6. causam `activities_changed` por SSE após resposta HTTP bem-sucedida;
7. seguem ao agente no próximo heartbeat cuja revisão esteja atrasada;
8. o agente substitui a política local e relata a revisão no ciclo seguinte.

Limitação atual: o histórico humano não mantém uma atividade pendente até
`applied_policy_revision` alcançar a revisão esperada. Para avaliar melhorias,
esta é a principal diferença em relação aos comandos confirmados.

### 9.6 Bônus local originado no agente

```text
Interface local ── D-Bus + senha ──► Agente
Agente ── persiste bônus + pending_event ──► SQLite local
Agente ── heartbeat(events) ──► Servidor
Servidor ── INSERT OR IGNORE + acknowledged_events ──► Agente
Servidor ── activity_updated ──► ADM
Agente ── remove pending_event após confirmação ──► SQLite local
```

Regras:

- a senha é validada pelo agente root, nunca pela interface local;
- opções atuais da interface local: 15, 30, 60 e 120 minutos;
- cada concessão gera UUID próprio e evento `bonus_added`;
- o evento usa origem `local`, data local e no máximo 12 horas;
- o agente envia até 100 eventos por heartbeat;
- falha incrementa `retry_count`, mas não remove o evento;
- o servidor usa o UUID como chave idempotente em bônus, auditoria e atividade;
- a atividade humana nasce concluída com etapas `local_created/device`,
  `synchronized/server` e `confirmed/server`;
- o agente remove o evento somente quando seu UUID voltar em
  `acknowledged_events`.

## 10. Estado vivo e offline

O servidor calcula o snapshot `deviceLiveStatus` a partir de banco, política e
horário atual. Ele contém pelo menos:

- data local considerada;
- cota, bônus, uso e saldo;
- online/offline;
- sessão gráfica ativa;
- contagem ativa;
- revisões desejadas e aplicadas;
- estado desejado, estado observado e situação do controle.

Um dispositivo está online quando:

```text
last_seen_at >= agora - online_timeout
```

Online não significa sessão gráfica ativa. Contagem só aparece ativa quando a
decisão da política permite contar, o agente está online, existe sessão gráfica
ativa e ela não está travada.

O detector de offline roda a cada `online_timeout/2`, com mínimo de 1 segundo,
mas só publica para dispositivos com assinantes SSE. Ele emite
`device_offline` uma vez por período de ausência. O heartbeat seguinte publica
`status` e naturalmente restaura a visão online.

## 11. SSE servidor → ADM

### 11.1 Propriedades

- endpoint por dispositivo;
- exige sessão administrativa válida;
- usa `EventSource` com credenciais;
- `Content-Type: text/event-stream`;
- `Cache-Control: no-store`;
- `X-Accel-Buffering: no`;
- keep-alive `: ping` a cada 15 segundos;
- a sessão é revalidada a cada ping;
- encerramento da sessão encerra o stream;
- cada assinante tem buffer de 8 eventos;
- publicadores nunca bloqueiam; evento pode ser descartado para assinante
  lento.

SSE é aceleração de interface, não fila durável. A verdade continua no banco e
nas APIs de consulta.

### 11.2 Eventos atuais

| Evento | Conteúdo | Uso no ADM |
| --- | --- | --- |
| `hello` | snapshot vivo | hidratar conexão/reconexão |
| `status` | snapshot vivo após heartbeat | atualizar online, sessão, tempo e saldo |
| `device_offline` | snapshot com offline | marcar ausência |
| `activity_updated` | atividade completa | atualizar uma operação end-to-end |
| `activities_changed` | `{device_id}` | recarregar lista durável após mutação |
| `communication` | registro técnico | acrescentar diagnóstico sem polling |

O ADM abre um único stream para o dispositivo selecionado. Ao trocar de
dispositivo, fecha o anterior. `onopen` marca atualização ao vivo e força nova
consulta das listas. `onerror` mostra conexão interrompida; o `EventSource`
tenta reconectar segundo o comportamento do navegador.

Eventos podem chegar antes ou depois da resposta HTTP que iniciou a operação.
Por isso, toda projeção deve ser idempotente e a reconciliação deve comparar o
ID e o progresso da atividade.

## 12. Três registros diferentes

### 12.1 Fonte de verdade de negócio

Tabelas como `policy`, `device_control`, `device_command`, `bonus`,
`daily_usage` e `routine` definem o estado real. Nenhuma exclusão de histórico
pode alterar essas tabelas.

### 12.2 Atividade humana

`activity` e `activity_step` formam uma projeção legível. Estados possíveis:

- `waiting_device`;
- `offered`;
- `completed`;
- `attention`;
- `failed`.

Etapas atuais incluem:

- `requested/admin`;
- `stored/server`;
- `offered/server`, com ocorrências agregadas;
- `completed/device`;
- `requested/admin` + `completed/server` para ações só do servidor;
- `local_created/device`, `synchronized/server` e `confirmed/server` para bônus
  local.

Limitação atual: o schema prevê `attention` e `failed`, mas o protocolo v2 só
carrega ack de sucesso. O agente não envia resultado estruturado de falha
definitiva; por isso esses estados ainda não descrevem toda falha local
possível.

Limpar ações concluídas preenche `hidden_at`. Não remove bônus, configurações
ou pedidos pendentes. Atividades concluídas são removidas fisicamente após 30
dias; a manutenção roda no máximo uma vez por hora. Envelopes de controle já
confirmados são descartados imediatamente, sem apagar essa atividade humana.

### 12.3 Auditoria

`audit_event` guarda fatos funcionais sanitizados e ajuda migrações ou análise
histórica. A atividade humana pode ser reconstruída apenas quando há
correlação inequívoca. Não se deve inventar confirmação retroativa.

### 12.4 Diagnóstico técnico

`communication_log` registra intercâmbios entre `interface`, `api` e `agent`:

- operação;
- resultado `success`, `warning` ou `error`;
- status HTTP;
- duração;
- resumo humano;
- detalhes técnicos sanitizados;
- correlação e horário.

O servidor registra heartbeats e respostas que transportaram estado. Para o
ADM, leituras automáticas de dispositivo, status, comandos, eventos,
atividades, comunicação e stream não geram atividade humana. Mutações
bem-sucedidas nos recursos de negócio de um dispositivo publicam
`activities_changed`; operações que limpam o próprio histórico/diagnóstico e
o cadastro inicial de um dispositivo são reconciliadas diretamente pela tela.

Chaves contendo `password`, `token`, `authorization`, `cookie`, `secret`,
`payload` ou `body` são rejeitadas pelo storage. Há também limites de tamanho e
quantidade de campos.

O diagnóstico pode ser apagado fisicamente por dispositivo. Sua retenção é
global, limpa registros antigos durante novas inserções no máximo uma vez por
hora e limpa imediatamente quando o período é reduzido.

## 13. Semântica de erros

### 13.1 Heartbeat

| Status | Significado |
| ---: | --- |
| `200` | heartbeat aceito; resposta pode conter mudanças |
| `400` | método, versão, JSON, campos ou estado inválidos |
| `401` | device ID/token inválidos ou revogados |
| `409` | revisão local maior que a revisão central |
| `426` | agente v1 incompatível com bônus remoto pendente |
| `500` | servidor não conseguiu inspecionar ou persistir estado |

O agente classifica falhas por estágio:

- `local_state`: leitura/validação local;
- `request`: montagem da requisição;
- `transport`: DNS, TLS, conexão ou timeout;
- `response`: status/JSON de resposta;
- `apply`: persistência ou aplicação local.

A interface local traduz erros sanitizados em orientações humanas para
incompatibilidade, credencial, revisão, indisponibilidade, DNS e timeout.

### 13.2 ADM

| Status | Significado comum |
| ---: | --- |
| `200/201/204` | concluído no servidor |
| `202` | assíncrono persistido, aguardando agente |
| `400` | entrada inválida |
| `401` | sessão ausente ou expirada |
| `403` | origem ou CSRF recusado |
| `404` | dispositivo/recurso inexistente ou rota incorreta |
| `409` | conflito de rotina ou setup já realizado |
| `500` | falha interna |

Se uma ação pode ter sido salva e a renderização do ADM falhar depois, a tela
de recuperação deve orientar a recarregar o estado do servidor. A interface
não deve repetir automaticamente uma mutação ambígua.

## 14. Idempotência, ordem e transações

Uma reprodução compatível deve preservar estas regras:

1. UUID de evento local é chave única no agente e no servidor.
2. O ID da operação é compartilhado pelo envelope de comando, pela confirmação
   em `applied_command` e pela atividade correspondente; o envelope de controle
   pode ser descartado depois do ack.
3. Aplicar bônus remoto e registrar comando aplicado é uma transação local.
4. Criar ou substituir comando de controle, alterar estado desejado e criar sua
   atividade é uma transação no servidor.
5. Uso diário central nunca diminui por heartbeat atrasado.
6. Política só é substituída por revisão completa válida.
7. Evento pendente só sai da fila após confirmação explícita.
8. Bônus permanece pendente até ack; para controle, somente a intenção mais
   recente permanece pendente e seu envelope é descartado depois do ack.
9. Comandos são oferecidos por `created_at, id`, no máximo 100 por resposta e
   no máximo um deles é controle do dispositivo.
10. Eventos são enviados pelo agente em ordem de criação, no máximo 100.
11. `INSERT OR IGNORE` ou equivalente deve tornar repetição inofensiva.
12. Falha na projeção humana não deve impedir entrega de comando já válido; o
    histórico é derivado, não autoridade de negócio.

## 15. Modelo mínimo de persistência para replicação

### 15.1 Agente

Uma implementação equivalente precisa manter:

- `policy_state`: política e revisão aplicada;
- `weekly_quota`;
- `routine` e `routine_day`;
- `daily_usage`: uso por data local;
- `bonus`: créditos locais e remotos idempotentes;
- `pending_event`: eventos locais ainda não confirmados;
- `applied_command`: comandos já aplicados;
- `confirmed_session_state`: última âncora de saldo;
- `enrollment`: servidor e dispositivo associados.

Credencial do dispositivo permanece na configuração root, não em mensagens ou
logs da aplicação.

### 15.2 Servidor

Uma implementação equivalente precisa manter:

- administradores e hashes de senha;
- dispositivos e hash de token;
- política, cotas, rotinas e dias;
- controle e revisões desejadas/aplicadas;
- uso diário e bônus;
- bônus pendentes e a última intenção de controle, com ack, tentativas e
  horários de oferta;
- auditoria funcional;
- atividade humana e suas etapas;
- diagnóstico técnico e configuração de retenção;
- controle de migrações e manutenção.

Relacionamentos por dispositivo devem usar exclusão em cascata onde aplicável.
Excluir um dispositivo elimina seu histórico; não existe uma atividade
remanescente de “dispositivo excluído” depois da exclusão.

## 16. Requisitos de implantação

Para reproduzir o projeto:

- executar o agente como serviço nativo privilegiado; não em Docker;
- proteger banco e configuração do agente para root;
- servir API separadamente do ADM;
- servir o build estático do ADM em Nginx ou equivalente;
- configurar `admin_origin` de modo coerente com a origem real;
- ativar `secure_cookies` e HTTPS fora de ambiente local controlado;
- manter SQLite do servidor em volume persistente;
- desativar buffering de proxy para SSE;
- configurar timeout de leitura do proxy acima do keep-alive;
- não impor `WriteTimeout` que encerre o stream ativo;
- executar todas as migrações antes de aceitar tráfego;
- garantir relógio razoável nos hosts, preservando a data local enviada pelo
  agente para consumo e bônus.

## 17. Critérios mínimos de compatibilidade

Uma implementação alternativa só deve ser considerada compatível se passar,
no mínimo, pelos seguintes cenários:

1. heartbeat autenticado sem mudanças;
2. política atrasada entregue e aplicada por revisão;
3. sessão nova recebe âncora sem restaurar consumo posteriormente;
4. uso repetido ou fora de ordem não diminui o consolidado;
5. bônus remoto confirmado na primeira oferta;
6. bônus remoto reenviado várias vezes sem duplicar saldo;
7. agente offline recebe operação depois de reconectar;
8. ack perdido é reenviado e conclui a mesma operação;
9. duas submissões humanas reais geram duas operações distintas;
10. bônus local offline sincroniza depois sem duplicação;
11. token revogado recebe `401`;
12. revisão local adiantada recebe `409` sem sobrescrita;
13. agente antigo com bônus pendente recebe `426`;
14. SSE reconecta e reconcilia o estado por HTTP;
15. dispositivo passa a offline após timeout e volta no heartbeat seguinte;
16. limpar atividade não altera negócio;
17. expiração remove apenas atividades concluídas;
18. diagnóstico rejeita qualquer segredo;
19. rotina sobreposta recebe conflito legível;
20. falha visual do ADM não repete mutação automaticamente.

## 18. Limitações atuais e oportunidades de melhoria

### 18.1 Confirmação de política

Atividades de cotas, rotinas e senha terminam no servidor, não na revisão
aplicada pelo agente. Melhoria recomendada: guardar `expected_revision` e
concluir outra etapa quando `applied_policy_revision >= expected_revision`.

### 18.2 Falha estruturada de comando

`command_acks` prova sucesso, mas ausência de ack não distingue demora de falha
definitiva. Melhoria recomendada: adicionar `command_results` com status e
códigos fechados, mantendo ack compatível.

### 18.3 Replay SSE

O stream não usa `id:` nem `Last-Event-ID`. Um assinante lento pode perder
evento. A mitigação atual é recarregar REST. Melhoria possível: sequência
durável ou replay limitado, sem transformar SSE em fonte de verdade.

### 18.4 Sessões administrativas em memória

Reiniciar o servidor encerra todas as sessões. Isso é seguro, mas pode ser
inconveniente ou incompatível com múltiplas réplicas. Uma evolução exige
sessões persistentes ou assinatura verificável e revogável.

### 18.5 Volume do diagnóstico

Todo heartbeat gera registro técnico, embora não gere atividade humana. Medir
volume e custo pode justificar agregação de heartbeats normais, preservando
falhas, transições e respostas com conteúdo.

### 18.6 Prova de entrega

`delivery_attempts` conta quando o servidor prepara a resposta, não quando o
agente comprova recepção. A linguagem da interface já evita afirmar além dessa
evidência. Resultado estruturado ou número de sequência poderia tornar a
distinção ainda mais precisa.

### 18.7 Exclusão do dispositivo

A exclusão em cascata remove também o próprio histórico da exclusão. Se houver
exigência de auditoria global, será necessária tabela desvinculada do ciclo de
vida do dispositivo, com política explícita de privacidade e retenção.

## 19. Rastreabilidade no código

| Tema | Fonte principal |
| --- | --- |
| Tipos e versão do heartbeat | `protocol/v1/sync.go` |
| Cliente, retry e aplicação | `agent/syncclient/client.go` |
| Idempotência local | `agent/storage/sync.go` |
| Configuração do agente | `agent/config/config.go` |
| D-Bus e diagnóstico local | `agent/localapi/dbus.go` |
| Rotas administrativas | `server/web/admin_api.go` |
| Heartbeat HTTP | `server/web/api.go` |
| SSE e offline | `server/web/events.go` |
| Estado vivo | `server/web/status.go` |
| Log técnico | `server/web/communication.go` e `server/storage/communication.go` |
| Transação do heartbeat | `server/storage/sync.go` |
| Política e domínio | `server/storage/domain.go` |
| Atividade humana | `server/storage/activities.go` |
| Cliente HTTP/SSE do ADM | `admin-ui/src/api.ts` |
| Orquestração do ADM | `admin-ui/src/App.tsx` |
| Histórico humano/técnico | `admin-ui/src/communication/CommunicationPage.tsx` |
