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
| 3. Agente e carga | pendente | UUID, vínculo, rotação, erros e heartbeat eficiente passam nos testes |
| 4. Abertura do piloto | pendente | suíte, restauração e carga de 500 agentes aprovadas antes da segunda família |

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

## Próximo passo exato

Criar a migração do SQLite local para UUID v4 e fingerprint SHA-256 do token,
preservando política, uso e eventos de um vínculo antigo. Em seguida fazer o
cliente enviar o UUID e a capacidade no heartbeat, com testes de atualização,
troca apenas de token e troca de servidor/dispositivo, antes de alterar a
aceitação no servidor.

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
