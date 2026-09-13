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
| 1. Isolamento | em andamento | migração preserva dados e testes entre duas famílias impedem qualquer acesso ou efeito cruzado |
| 2. Entrada autônoma | pendente | cadastro, confirmação, login, recuperação, exclusão e limite de cinco dispositivos funcionam sem operador |
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

## Próximo passo exato

Adicionar `family_id` e `auth_generation` à sessão, revalidá-la no banco em
cada requisição e SSE, e migrar listagem/criação/rotas administrativas para
sempre autorizar `family_id + device_id` antes de qualquer leitura ou efeito.

## Validação acumulada

- `go test ./server/storage` — aprovado após a migração `0015`.
