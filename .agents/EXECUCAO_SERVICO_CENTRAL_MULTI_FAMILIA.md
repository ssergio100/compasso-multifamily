# Diário de execução — serviço central multi-família

Atualize este arquivo ao concluir ou interromper qualquer bloco de trabalho.
Ele é o ponto de retomada para outra sessão; a especificação detalhada e o
plano original permanecem em `.private/notes/`.

## Objetivo e limite

Entregar, na branch `feat/servico-central-multifamilia`, um piloto central de
até 100 famílias e 500 dispositivos. Cada família terá uma conta proprietária,
até cinco dispositivos, cadastro online com e-mail confirmado e isolamento
total das demais famílias.

Continuam vigentes:

- `admin-ui`: única interface web administrativa;
- `local-ui`: somente configuração local do agente e bônus local;
- uma API, um SQLite, sessões e SSE em memória;
- configuração manual do agente com `device_id` e `device_token`;
- uma instalação ativa por dispositivo.

Não ampliar o trabalho para múltiplos responsáveis, login social, 2FA,
pareamento automático, múltiplas réplicas, PostgreSQL, Redis, WebSocket,
inventário de hardware ou interfaces descontinuadas.

## Decisões que não devem ser rediscutidas sem novo requisito

- Cadastro normal é autônomo; o operador atua apenas em suspensão, abuso ou
  incidente.
- E-mail normalizado é o login. Confirmação vence em 24 horas e recuperação de
  senha em 30 minutos; tokens são aleatórios, de uso único e armazenados como
  hash.
- O limite de 100 famílias é aplicado atomicamente na confirmação. Conta não
  confirmada não ocupa vaga e expira após 24 horas.
- Toda operação administrativa de dispositivo recebe `family_id` da sessão e
  consulta por `family_id + device_id`; referência cruzada responde `404` sem
  escrita, log funcional ou SSE na família alvo.
- O agente usa UUID v4 persistido no SQLite local. Atualização preserva o UUID;
  instalação nova ou token novo gera outro.
- O servidor vincula atomicamente o primeiro UUID válido. Outro UUID recebe
  `409 installation_conflict`. Agente antigo recebe
  `426 agent_upgrade_required` quando a capacidade passar a ser obrigatória.
- Implantação da identidade é sempre agente primeiro, servidor depois.
- Heartbeat normal: 5 s com sessão gráfica e 30 s sem sessão, com jitter de até
  10%, backoff e `online_until` calculado pelo servidor.

## Marcos

| Marco | Estado | Evidência para concluir |
| --- | --- | --- |
| 0. Preparação | concluído | branch criada e diário versionável iniciado |
| 1. Isolamento | concluído | migração preserva dados; duas famílias não acessam dados, efeitos, logs ou SSE entre si; suspensão local validada |
| 2. Entrada autônoma | concluído | cadastro, confirmação, login, recuperação, dispositivo e exclusão passam no fluxo completo sem operador |
| 3. Agente e carga | concluído | UUID, vínculo, rotação, erros e heartbeat eficiente passam nos testes |
| 4. Abertura do piloto | aguarda ambiente público | suíte, restauração e carga aprovadas; faltam valores e validação do serviço real antes da segunda família |

## Checklist de retomada

- [x] Criar branch dedicada e diário versionado.
- [x] Migrar família/conta/dispositivo preservando a instalação existente.
- [x] Isolar leituras, mutações, atividades, diagnóstico e SSE por família.
- [x] Revalidar sessões e implementar suspensão/reativação local.
- [x] Entregar cadastro autônomo, confirmação, recuperação, troca de e-mail,
  troca de senha e exclusão da família.
- [x] Aplicar limites atômicos de 100 famílias e cinco dispositivos.
- [x] Exigir senha atual para token e exclusões; integrar SMTP com STARTTLS.
- [x] Persistir UUID/fingerprint no agente e enviar a capacidade no heartbeat.
- [x] Gravar o checkpoint agente-primeiro antes da exigência no servidor.
- [x] Implementar vínculo atômico, conflito, rotação/revogação e
  `online_until` com intervalos 5/30 segundos.
- [x] Implementar jitter, esperas por classe de erro, limites de heartbeat,
  métricas agregadas e redução inicial de escrita/log/SSE.
- [x] Concluir testes focados e a suíte integral do marco 3; corrigir qualquer
  regressão encontrada.
- [x] Fechar a confirmação de e-mail da conta migrada e as operações locais
  excepcionais necessárias antes do piloto.
- [x] Validar backup e restauração em banco migrado.
- [x] Executar o cenário de carga equivalente a uma hora com 500 agentes,
  registrar p95, erros e crescimento do banco.
- [ ] Validar configuração pública (HTTPS, cookies, origem, SMTP e exigência de
  identidade), executar `make test` final e registrar a decisão de abertura.

## Estado inicial confirmado

- Base da branch: commit `598f438` (`main` estava um commit à frente de
  `origin/main`).
- Banco do servidor possui migrações `0001` a `0014`; ainda não existe família
  nem vínculo de instalação.
- `admin_user` usa `login`, senha Argon2id e estado ativo.
- O setup web cria somente o primeiro administrador e o bootstrap por ambiente
  faz o mesmo.
- Sessão guarda apenas ID/login/CSRF/expiração e não é revalidada no banco a
  cada requisição.
- Métodos administrativos de storage recebem apenas `device_id`; portanto o
  isolamento deve começar nessa fronteira, não apenas nos handlers.
- Heartbeat autentica `device_id + token`; sessões, SSE e presença ainda usam o
  comportamento do serviço de uma família.
- A suíte completa passava antes do início deste trabalho.

## Histórico

### 2026-09-13 — preparação

- Criada a branch `feat/servico-central-multifamilia` a partir de `598f438`.
- Confirmadas as interfaces vigentes pelas instruções do repositório.
- Confirmado que `.private/` é ignorado; este diário foi colocado em `.agents/`
  para acompanhar a branch.
- Nenhuma mudança funcional realizada até este ponto.

### 2026-09-13 — base de dados do marco 1

- Adicionada a migração `0015_multi_family.sql` com `family`,
  `family_member`, campos de conta, geração/vínculo do dispositivo e
  `account_token`.
- Banco vazio continua vazio. Banco com um administrador cria uma família,
  associa todos os dispositivos e converte tokens existentes para geração 1.
- Migração falha atomicamente quando encontra mais de um administrador ou
  dispositivo sem um único administrador responsável.
- `family_id` é exigido por chave estrangeira e triggers também em SQLite já
  populado; dispositivo órfão não pode ser inserido.
- O bootstrap atual passou a criar família e proprietário na mesma transação.
- Criada `CreateDeviceForFamily`, com validação da família ativa e limite
  transacional de cinco dispositivos. A função antiga é apenas ponte enquanto
  os chamadores são migrados e falha se não houver exatamente uma família.

### 2026-09-13 — marco 1 concluído: isolamento por família

- A sessão em memória passou a carregar `family_id` e `auth_generation`. Toda
  requisição, inclusive cada keep-alive SSE, recarrega conta, associação e
  estado da família; suspensão ou mudança de geração remove a sessão.
- Listagem e criação usam a família da sessão. A sexta criação responde
  conflito e não insere dados.
- Criado `storage.FamilyDevice`, uma capacidade não forjável fora do storage.
  Ela nasce somente após consulta por `family_id + device_id` e revalida esse
  par em cada leitura, mutação, atividade, auditoria ou diagnóstico pedido por
  um administrador. As operações autenticadas do agente continuam separadas.
- Todos os handlers administrativos de dispositivo, o snapshot/keep-alive SSE,
  e as publicações originadas pela interface recebem o dispositivo já
  autorizado. IDs brutos da URL não chegam a middlewares de gravação.
- Tentativas cruzadas retornam `404` antes de interpretar o corpo. Teste com
  duas famílias percorre detalhe, estado, política, rotinas, senha, token,
  bônus, comandos, auditoria, atividades, diagnóstico e stream, confirmando
  ausência de alteração, auditoria, diagnóstico e assinatura SSE no alvo.
- Adicionados os comandos locais `-suspend-family` e
  `-reactivate-family`, por login do proprietário ou ID da família. Cada
  mudança incrementa `auth_generation`; por isso uma sessão antiga não revive
  após a reativação. O uso via Docker Compose está documentado.
- A retenção de diagnóstico deixou de ser mutável pela família: a API familiar
  apenas informa o valor global e a `admin-ui` vigente o mostra como leitura.
  A limpeza dos registros do próprio dispositivo permanece disponível.
- A primeira execução integral encontrou quatro fixtures de `agent/syncclient`
  que ainda criavam dispositivo em banco vazio. O helper compartilhado agora
  cria a família proprietária antes do dispositivo; os quatro testes passaram.

### 2026-09-13 — marco 2 concluído: entrada autônoma

- A migração `0016_pending_account.sql` acrescenta somente o nome temporário da
  família à conta pendente e o cascade necessário para excluir a família sobre
  a coluna adicionada em SQLite. O esquema atual passa a ser a versão 16.
- Cadastro cria apenas identidade inativa e token de confirmação com hash. A
  família e a associação proprietária nascem juntas ao confirmar; o limite de
  100 famílias é verificado nessa mesma transação. Pendências vencidas são
  removidas por manutenção horária e também antes de novo cadastro/reenvio.
- Foram implementados confirmação e reenvio em 24 horas, recuperação em 30
  minutos, troca de senha, troca confirmada de e-mail e exclusão da própria
  família com senha atual e nome exato. Tokens são aleatórios, de uso único e
  persistidos apenas como SHA-256; mudanças de credencial invalidam sessões.
- O antigo `/api/v1/admin/setup` foi removido. A `admin-ui` vigente oferece
  Criar conta, confirmar, recuperar senha e Minha conta. Um proprietário novo
  cria um dispositivo, revela seu token uma vez e exclui família e dispositivo
  sem intervenção humana no teste ponta a ponta.
- Cadastro, reenvio e recuperação respondem sem enumerar contas. Limites em
  memória cobrem e-mail, origem de rede, conclusão por token e login; dez
  falhas de login em 15 minutos bloqueiam o login por 15 minutos. O mapa de
  limites elimina chaves inativas para não crescer indefinidamente.
- Emissão/revogação de token e exclusão de dispositivo agora exigem a senha
  atual além da sessão e CSRF. Testes demonstram que senha incorreta não gira
  credencial nem exclui cadastro.
- Adicionado envio SMTP substituível, com STARTTLS e TLS 1.2 mínimos. Sem SMTP,
  apenas os fluxos que dependem de e-mail respondem `503`; endereço, remetente
  e credenciais são fornecidos por ambiente e a operação está documentada.
- Corrigido o escopo do cookie anti-CSRF anônimo para `/api/v1`, permitindo que
  o navegador o envie ao cadastro sem ampliar o cookie autenticado, que segue
  restrito a `/api/v1/admin`.
- Fechada uma pendência da suspensão: depois de validar o token do agente, o
  heartbeat de família suspensa responde `403 family_suspended`; token inválido
  continua recebendo `401` sem revelar o estado da família.

### 2026-09-13 — marco 3, checkpoint do agente compatível

- Adicionada a migração local `0006_installation_identity.sql`. O vínculo
  guarda UUID v4 e somente o fingerprint SHA-256 do token; a configuração com
  o segredo não foi duplicada no banco.
- Um banco antigo com vínculo existente recebe UUID e fingerprint sem perder
  política. Reinício e atualização com os mesmos dados preservam o UUID; troca
  apenas do token gera outro UUID e preserva política/uso; troca de servidor ou
  dispositivo gera outro UUID e limpa o estado local do vínculo anterior.
- O agente envia `X-Compasso-Agent-Installation-ID` e anuncia a capacidade
  `installation-identity`. A validação compartilhada aceita apenas UUID v4
  canônico. O servidor ainda não exige nem vincula esse cabeçalho neste
  checkpoint, preservando a ordem agente-primeiro da implantação.

### 2026-09-14 — marco 3 concluído: vínculo e controle de carga

- O servidor vincula o primeiro UUID v4 dentro da mesma transação de um
  heartbeat válido. Outro UUID recebe `409 installation_conflict` antes da
  decodificação completa; rotação e revogação incrementam a geração, limpam o
  vínculo e a presença, sem apagar política ou consumo central.
- A exigência de `installation-identity` é negociável por
  `COMPASSO_REQUIRE_INSTALLATION_IDENTITY` e fica desativada durante a etapa
  agente-primeiro. Quando ativa, agente antigo recebe
  `426 agent_upgrade_required`.
- O heartbeat retorna 5 segundos com sessão gráfica e 30 segundos sem sessão.
  O servidor grava `online_until` por `max(60 s, 4 x intervalo)`; o painel usa
  esse prazo em vez do timeout global para agentes compatíveis.
- O agente acrescenta jitter de 0% a 10%. Rede e `5xx` usam backoff exponencial
  até cinco minutos, `429` respeita `Retry-After` entre cinco segundos e uma
  hora, erros permanentes aguardam seis horas e outros `4xx`, uma hora.
- A aplicação limita 50 heartbeats/s globalmente, dez credenciais inválidas/s
  por origem com rajada de vinte e, quando a identidade é obrigatória, um
  heartbeat/s por dispositivo com rajada de dois.
- Heartbeat saudável sem mudança não cria histórico de comunicação, não
  regrava uso diário idêntico e só publica o mesmo status SSE a cada trinta
  segundos. `/metrics` expõe apenas contadores agregados e histograma de
  latência.
- O ensaio reproduzível `TestPilotLoadOneLogicalHour500Agents` avançou uma hora
  lógica com 500 agentes: 100 ativos a cada 5 s e 400 ociosos a cada 30 s.
  Foram 120.000 respostas `200`, nenhum `5xx`, p95 de 2,380056 ms e crescimento
  do conjunto SQLite de 163.840 bytes (6.422.680 para 6.586.520 bytes). A
  execução real durou 238,60 s com `_synchronous=FULL`.
- O teste de carga é propositalmente opt-in para não transformar toda suíte em
  um ensaio de quatro minutos: execute com
  `COMPASSO_RUN_PILOT_LOAD_TEST=1 go test -v ./server/web -run
  '^TestPilotLoadOneLogicalHour500Agents$' -count=1 -timeout=30m`.

### 2026-09-14 — fechamento operacional anterior ao piloto

- Uma conta vinda do banco antigo continua entrando uma última vez com o login
  anterior. A `admin-ui` vigente aceita esse identificador, abre **Minha conta**
  automaticamente e orienta a confirmação do e-mail. Ao confirmar, o login é
  substituído pelo e-mail normalizado e as sessões antigas são invalidadas.
- A confirmação de uma nova família verifica na mesma transação se todos os
  proprietários existentes já possuem e-mail confirmado. Assim, a segunda
  família não entra enquanto a conta migrada estiver incompleta; o fluxo normal
  de cadastro continua autônomo e não depende do operador.
- Adicionado o comando local `-delete-suspended-family ID
  -confirm-family-id ID`. Ele recusa família ativa, confirmação divergente e ID
  ausente; remove a família suspensa e seus dados apenas quando os dois IDs
  opacos são exatamente iguais.
- `scripts/test-backup-restore.sh` cria um banco temporário na versão 16,
  arquiva o diretório `server/`, altera o original e restaura a cópia. O ensaio
  aprovou checksum idêntico, conteúdo, 16 migrações, `integrity_check=ok` e
  nenhuma violação de chave estrangeira sem tocar em Docker ou dados reais.
- O pacote agora inclui `compasso-server-backup.timer`, diário, persistente e
  com atraso aleatório de até uma hora. O instalador o ativa somente após a API
  ficar saudável. Os backups não são apagados automaticamente, preservando a
  retenção mínima; espaço disponível continua sendo item de monitoramento.
- O pacote `compasso-server_0.1.0~pilot31_all.deb` foi montado e validado com os
  dois units de backup. SHA-256:
  `3dcf90684a9d6106309c3d31b731829c1695b951a43233a7ef9c85775ea54bf6`.

### 2026-09-14 — auditoria final local e fronteira da abertura

- O `admin_user_id` autenticado passa da sessão para a capacidade
  `FamilyDevice`; ele não é aceito do corpo da requisição. Auditorias de
  criação, política, rotina, senha, credencial, bônus e identidade, além das
  atividades de comandos, registram esse autor nos detalhes duráveis.
- A exclusão de dispositivo ganhou auditoria transacional antes do `DELETE`;
  o evento funcional sobrevive com o autor, enquanto os dados pertencentes ao
  dispositivo continuam seguindo o cascade previsto.
- Testes ponta a ponta conferem que o autor gravado é exatamente o ID da conta
  proprietária presente na sessão, tanto em `audit_event` quanto em
  `activity`.
- O exemplo de produção já exige `secure_cookies=true` e origem HTTPS absoluta;
  o SMTP valida STARTTLS/TLS 1.2, e a identidade pode ser exigida depois da
  atualização agente-primeiro. Esses mecanismos foram testados, mas seus
  valores e funcionamento público dependem do domínio, relay e máquinas reais.
- `make test` final aprovado: `go vet`, todos os pacotes Go, 22 testes da
  interface local, typecheck/build da `admin-ui`, seis migrações do agente, 16
  do servidor, backup/restauração, hardening, links e builds.
- A segunda família continua proibida operacionalmente. Para liberar o piloto
  ainda é necessário, no ambiente real: validar certificado/domínio e origem,
  confirmar envio e recuperação pelo SMTP, atualizar os agentes existentes,
  ativar `COMPASSO_REQUIRE_INSTALLATION_IDENTITY=true`, conferir `/metrics`,
  timer/arquivo de backup e espaço disponível, e confirmar o e-mail da conta
  migrada.

## Próximo passo exato

Gravar o checkpoint final local. A próxima sessão não deve alterar novamente o
produto para “resolver” a abertura: deve receber o destino público e os valores
operacionais autorizados, implantar na ordem agente primeiro/servidor depois e
executar o checklist real acima. Somente após essas evidências deve marcar o
marco 4 e liberar a segunda família.

## Validação acumulada

- `go test ./server/storage` — aprovado após a migração `0015`.
- `go test ./server/storage ./server/cmd/tempo-server` — aprovado com
  isolamento, limite de dispositivos e suspensão/reativação.
- testes web focados — aprovados para sessão durável e todas as rotas cruzadas.
- `npm --prefix admin-ui run typecheck` e `npm --prefix admin-ui run build` —
  aprovados após tornar a retenção somente leitura.
- `make test` — aprovado integralmente ao concluir o marco 1 (Go, 22 testes da
  interface local, `admin-ui`, 15 migrações do servidor, hardening,
  documentação e builds).
- `make test` — aprovado integralmente ao concluir o marco 2 (todos os pacotes
  Go, 22 testes da interface local, typecheck/build da `admin-ui`, 16 migrações,
  hardening, links de documentação e builds dos três binários).
- `go test ./agent/storage ./agent/syncclient ./agent/cmd/tempo-agent
  ./protocol/v1` e `./scripts/test-migrations.sh` — aprovados no checkpoint
  agente-primeiro; o banco local possui seis migrações.
- `go test ./agent/syncclient ./server/storage ./server/web
  ./server/cmd/tempo-server` — aprovado após o vínculo, os novos erros, limites,
  métricas e a redução de escrita/log/SSE do marco 3.
- teste de carga opt-in — aprovado com 500 agentes, uma hora lógica, 120.000
  heartbeats, zero erro, p95 de 2,380056 ms e 163.840 bytes de crescimento.
- `make test` — aprovado integralmente ao concluir o marco 3: `go vet`, todos
  os pacotes Go, 22 testes da interface local, typecheck/build da `admin-ui`,
  seis migrações do agente, 16 do servidor, hardening, documentação e builds.
- fluxos focados de conta migrada e exclusão suspensa — aprovados em storage,
  comando local, API e typecheck/build da `admin-ui` vigente.
- `scripts/test-backup-restore.sh` — aprovado sobre banco na versão 16;
  `scripts/test-security-packaging.sh` — aprovado com o timer diário.
- pacote do servidor `0.1.0~pilot31` — montado e aprovado pelo teste do artefato.
- autoria administrativa — aprovada em auditorias e atividades com o ID vindo
  da sessão autenticada.
- `make test` final — aprovado integralmente, agora incluindo o ensaio
  automático de backup/restauração.
