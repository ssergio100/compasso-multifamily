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

### 2026-09-14 — início da implantação pública paralela

- O ambiente existente em `192.168.18.10` foi mantido: API e painel antigos
  continuam saudáveis nas portas `8181` e `8182`.
- Criado um stack novo e independente em `/srv/docker/compose/family`, com
  banco vazio em `/srv/docker/volumes/family/server` e build estático em
  `/srv/sites/family-ui`. Os containers `family-api` e `family-ui` estão
  saudáveis e escutam somente em `127.0.0.1:8281` e `127.0.0.1:8282`.
- O painel usa `https://apifamily.smresume.com`; a API aceita como origem
  administrativa somente `https://family.smresume.com`, exige cookies seguros
  e mantém `TEMPO_REQUIRE_INSTALLATION_IDENTITY=false` para a coexistência
  inicial dos agentes.
- A configuração do Cloudflare Tunnel recebeu os dois ingressos e passou em
  `cloudflared tunnel ingress validate`. O arquivo anterior foi preservado em
  `/srv/cloudflare/config.yml.before-family-20260914`. Após a correção dos
  registros DNS, as regras foram limpas, o túnel foi reiniciado e os quatro
  endpoints novos e antigos responderam por HTTPS. Uma regra anterior ao
  ingresso da API responde `404` especificamente para `/metrics`, mantendo as
  métricas disponíveis apenas pelo endereço local.
- Instalado backup diário às `03:23` pelo `crontab` de `sergio`, sem container
  permanente adicional. O primeiro arquivo ficou em
  `/srv/docker/backups/family`, modo `0600`, e uma restauração descartável da
  cópia respondeu com sucesso ao healthcheck.
- `make test` passou integralmente antes do deploy. No servidor foram validados
  healthchecks locais, CORS da origem pública, configuração em tempo de
  execução do painel, métricas e hardening dos dois containers. Em navegador
  real, o painel público carregou login e criação de conta sem erro no console.
- O domínio de envio `notify.smresume.com` foi verificado no Resend e os quatro
  valores SMTP foram configurados sem expor a chave. A API foi recriada,
  reconheceu o mailer e permaneceu saudável. Conexão, STARTTLS, autenticação e
  envio passaram usando o endereço oficial `delivered@resend.dev`; ainda falta
  comprovar confirmação e recuperação com uma conta real do piloto.

### 2026-09-14 — distribuição pública do agente

- A `admin-ui` vigente passou a ler `/downloads/agent-release.json` em tempo de
  execução. O download aparece depois da confirmação de uma conta e na seção
  **Liberar acesso do agente**, junto da versão, arquitetura e SHA-256.
- Os estados curtos de e-mail enviado e confirmado ficam centralizados em telas
  pequenas; formulários permanecem alinhados à esquerda. O cadastro repete o
  e-mail normalizado informado e a recuperação mantém a resposta condicional
  para não enumerar contas.
- O assistente gráfico do pacote oficial sugere
  `https://apifamily.smresume.com`. A configuração efetiva continua sendo
  gravada somente quando `server_url`, `device_id` e `device_token` estiverem
  completos; atualizações preservam o conffile existente.
- Criado `scripts/publish-client-release.sh` e o alvo
  `make publish-client-release`. O fluxo compila os binários portáteis, gera e
  valida o `.deb`, impede substituir uma versão por conteúdo diferente, envia
  pacote e checksum e troca o manifesto por último. Versões antigas não são
  removidas automaticamente.
- O Nginx do `family-ui` passou a servir `/downloads/` sem fallback para a SPA e
  sem cache do manifesto. Configuração validada com `nginx -t`; arquivo ausente
  responde `404`.
- Publicado `compasso-client_0.1.0~pilot31_amd64.deb` em
  `https://family.smresume.com/downloads/`, SHA-256
  `3ab48851cc60c9ebe8e6e7cb1f5648d62e7f2c1e6de034c3710638c714a62bf3`.
  O pacote foi baixado novamente pelo endereço público, teve checksum,
  metadados e URL padrão conferidos, e o manifesto público retornou JSON com
  `Cache-Control: no-store`.
- A interface foi publicada em `/srv/sites/family-ui`; a inspeção no navegador
  não encontrou erros de console. `make test` passou integralmente depois das
  mudanças.

### 2026-09-14 — correção do fuso das rotinas

- Um caso real no Compasso anterior revelou divergência entre agente e painel:
  Arthur continuava contabilizando em horário de Brasília, mas a API em UTC
  apresentava **Contagem de tempo: Parada** ao considerar antecipadamente a
  rotina Dormir, das 22:00 às 08:00.
- As APIs `compasso` e `family` passaram a executar com
  `TZ=America/Sao_Paulo`. A mudança afeta a interpretação local de dias e
  horários; timestamps persistidos e enviados continuam em UTC. Não houve
  alteração de banco, saldo, política, interface ou agente.
- Somente os containers de API foram recriados. Os arquivos anteriores foram
  preservados em
  `/srv/docker/compose/compasso/compose.yaml.before-timezone-20260914` e
  `/srv/docker/compose/family/compose.yaml.before-timezone-20260914`.
- No Compasso, Arthur reconectou com sessão ativa e o uso avançou de 22.337 para
  23.193 segundos, compatível com o intervalo transcorrido e sem perda. No
  `family`, Zorin também reconectou com sessão ativa. As duas APIs ficaram
  saudáveis e os endpoints públicos do `family` responderam `200`.
- A primeira recriação do Compasso revelou que sua configuração durável ainda
  continha `COMPASSO_ADMIN_ORIGIN=same-host`, embora a interface já usasse a
  API pública separada. Isso fez a API responder `403 origin not allowed`.
  O `.env` foi corrigido para permitir somente
  `https://compasso.smresume.com` e usar cookies seguros; a versão anterior
  ficou em `/srv/docker/compose/compasso/.env.before-public-origin-20260914`.
  Pelo endereço público, a abertura de sessão passou a responder `200` com CORS
  restrito à origem correta, e um login com credenciais fictícias chegou à
  autenticação e respondeu `401`, em vez de `403`. Arthur reconectou novamente
  e o uso continuou avançando, sem perda.
- O Compose versionado agora expõe `COMPASSO_TIME_ZONE`, com padrão
  `America/Sao_Paulo`, e a validação de empacotamento impede remover essa
  configuração sem perceber. Se o serviço passar a atender simultaneamente
  famílias em fusos diferentes, o fuso deverá migrar de configuração da
  implantação para configuração por família ou dispositivo.

### 2026-09-14 — erros diagnosticáveis no `family`

- Implementado localmente um contrato uniforme para erros da API: toda resposta
  JSON de erro contém uma mensagem segura, um código estável e um identificador
  de correlação exclusivo. Os mesmos dados também seguem em cabeçalhos expostos
  somente à origem administrativa autorizada.
- A interface administrativa vigente traduz os códigos conhecidos para
  mensagens em português e apresenta `Código` e `Referência`. Falhas de rede,
  proxy, origem, CSRF, sessão e endereço incorreto agora são distinguíveis sem
  exibir detalhes internos nem permitir enumeração de contas.
- A API registra com a mesma referência somente falhas que exigem investigação:
  erros `5xx`, origem recusada, CSRF inválido e respostas sem código. Corpo,
  cookies, credenciais e query string não entram nesse log.
- Cobertura adicionada para código e correlação no corpo e nos cabeçalhos,
  unicidade da referência, rotas inexistentes e preservação de mensagens
  genéricas nos erros sensíveis. `make test` passou integralmente.
- Publicada a imagem `family-api:0.1.0-family2`; somente a API foi recriada e o
  volume `/srv/docker/volumes/family/server` permaneceu montado. A interface
  vigente também foi publicada em `/srv/sites/family-ui`. Os backups anteriores
  ficaram em
  `/srv/docker/compose/family/backups/source-before-diagnostics-20260914`,
  `env-before-diagnostics-20260914` e `ui-before-diagnostics-20260914`.
- O healthcheck público respondeu `200`. Uma chamada sem sessão respondeu `401`
  com `authentication_required`; uma origem inválida respondeu `403` com
  `origin_not_allowed`, e sua referência foi localizada sem divergência no log
  do container. No navegador, credenciais fictícias exibiram “E-mail ou senha
  inválidos”, o código e a referência; a única mensagem de console foi o `401`
  esperado dessa tentativa controlada.

### 2026-09-14 — pausa da contagem após logout

- Identificada a divergência no agente: a presença enviada no heartbeat já
  exigia uma sessão gráfica `active`, mas a contabilização local aceitava
  também sessões gráficas residuais que o `logind` mantém no estado `online`.
  Após o logout, o painel podia indicar sessão ausente enquanto o saldo local
  continuava sendo consumido.
- A contabilização, a situação exposta pelo daemon e o heartbeat agora usam a
  mesma definição: somente uma sessão gráfica local `active`. O intervalo já
  observado antes da transição é preservado uma única vez; ciclos posteriores
  ao logout não acrescentam uso.
- Adicionado teste de regressão com sessão ativa, consumo, transição para
  `online`, mais trinta segundos sem sessão ativa, persistência do saldo e
  estado preparado para o próximo heartbeat. O teste web também confirma
  `graphical_session_active=false` e `counting=false` depois desse heartbeat.
- O bloqueio de tela foi protegido por teste separado: uma sessão que continua
  `active`, mesmo bloqueada, mantém a contagem como antes. Não houve mudança de
  banco, protocolo, API nem interface.
- `make test` passou integralmente. Publicado o
  `compasso-client_0.1.0~pilot32_amd64.deb`, SHA-256
  `44891d1a968d6a60e36638e5a6e8dd3babeac0b81d4384e5424d219630f737eb`, em
  `https://family.smresume.com/downloads/`. O pacote, o checksum e o manifesto
  público foram validados; o `pilot31` foi preservado.

### Pendências confirmadas pelo uso real

1. **Erros diagnosticáveis — concluídos no `family`; Compasso pendente.** O
   mecanismo está implementado, publicado e validado ponta a ponta no `family`.
   Falta portar a correção de forma independente para o projeto antigo,
   preservando suas diferenças de implantação e interface.
2. **Fuso independente da localização do servidor.** O ajuste global para
   `America/Sao_Paulo` é somente o hotfix seguro para os dispositivos atuais.
   A solução definitiva deve guardar um fuso IANA por dispositivo, obtido do
   agente e validado pelo servidor, mantendo timestamps persistidos em UTC. A
   avaliação de dia da semana, rotinas e próximo bloqueio deve usar o fuso do
   dispositivo e continuar correta com horário de verão, mudança de local e
   servidor hospedado em qualquer país. Agentes antigos devem usar um fallback
   explícito durante a migração.
3. **Fim da sessão gráfica — implementado e publicado; validação real
   pendente.** O `pilot32` interrompe a contagem quando a sessão deixa de estar
   ativa e o painel recebe o mesmo estado no heartbeat. Falta validar o ciclo em
   uma máquina controlada real. Bloquear a tela continua sendo um evento
   diferente e não pausa a contagem.

## Próximo passo exato

Instalar o `pilot32` em uma máquina controlada e validar o caso real: anotar o
uso, encerrar a sessão gráfica, aguardar um heartbeat, confirmar sessão ausente
e contagem parada no painel e verificar que o uso não cresce. Entrar novamente
deve retomar a contagem sem perder nem duplicar o saldo anterior.

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
- `make test` — aprovado integralmente após o contrato de erros diagnosticáveis
  do `family`, incluindo Go, interface local, `admin-ui`, migrações, backup,
  hardening, documentação e builds.
- `make test` — aprovado integralmente após alinhar contabilização, heartbeat e
  apresentação ao estado gráfico `active`; o pacote `pilot32` também passou na
  validação Debian antes da publicação.
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
