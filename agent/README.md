# Agent

O `tempo-agent` é o daemon privilegiado instalado no computador controlado. Ele
avalia a última política local, contabiliza sessões gráficas e usa
`loginctl lock-session` quando uma regra bloquear o uso, preservando os
aplicativos abertos.

## Comportamento atual

- somente sessões gráficas locais `x11` ou `wayland` da conta configurada são
  contabilizadas;
- tela bloqueada pausa a contagem até a sessão gráfica ser destravada;
- TTY, SSH, sessões remotas, greeter e contas diferentes não contam;
- a ausência de rede não interfere no ciclo local;
- consumo é salvo a cada cinco segundos por padrão e no desligamento normal;
- uma sessão já em uso é bloqueada quando a política muda de liberada para
  bloqueada;
- uma sessão que surge durante um bloqueio não recebe a ação durante `opening`;
  depois de chegar a `active`, o agente aguarda dez segundos de estabilização;
- quando a sincronização está configurada, uma sessão nova sem saldo também
  aguarda o primeiro heartbeat concluído depois do login. Uma resposta com
  tempo libera a sessão; uma resposta que mantenha a restrição permite o
  bloqueio de tela.
  Falha de rede não conta como resposta e mantém a sessão aberta enquanto o
  agente tenta novamente.

O requisito atual permite a autenticação. Quando a política continuar
bloqueando, o agente espera a sessão gráfica ficar estabelecida e solicita o
bloqueio de tela pelo logind. Não encerra a sessão e não usa
`loginctl terminate-session` como fallback.

## Execução de desenvolvimento

Copie `config.example.toml`, ajuste `controlled_user` e use um banco que já
contenha uma política válida:

```bash
go run ./agent/cmd/tempo-agent -config ./agent/config.toml
```

O pacote `policy` contém o motor puro, `storage` mantém SQLite e checkpoints,
`session` usa logind para descoberta e bloqueio, e `daemon` coordena o ciclo. A
unidade de produção está em `packaging/systemd/tempo-agent.service`.

## Sincronização

Quando `server_url`, `device_id` e `device_token` estão configurados, o pacote
`syncclient` envia heartbeat, consumo e eventos pendentes. Políticas completas
são aplicadas por revisão e comandos são confirmados de forma durável. Se os
três valores estiverem vazios ou o servidor estiver indisponível, o daemon
continua aplicando integralmente o estado local.

O heartbeat anuncia `X-Compasso-Protocol-Version: 2`, as capacidades
`next-heartbeat-seconds`, `command-ack-receipts` e `installation-identity`, e
envia um UUID v4 próprio da instalação. Esse UUID fica no SQLite local: uma
atualização com a mesma configuração o preserva; troca de token ou nova
instalação gera outro. Quando o servidor devolve
`next_heartbeat_seconds`,
o agente usa esse intervalo no ciclo normal seguinte, limitado entre 1 segundo
e 10 minutos. Campo ausente ou inválido usa o fallback embutido de 3 segundos;
o valor não é persistido, e o `heartbeat_interval` local legado não controla o
processo instalado. Em um bônus remoto, o agente persiste a nova âncora antes
de registrar o comando como aplicado. O
reconhecimento enviado no heartbeat seguinte significa, portanto, que o saldo
autorizado já está durável; a data usada é a mesma data local enviada no
heartbeat, inclusive perto da meia-noite.

O servidor escolhe 5 segundos durante uma sessão gráfica e 30 segundos sem
sessão. O agente acrescenta jitter de até 10%. Falhas de rede e `5xx` usam
backoff exponencial até cinco minutos; `429` respeita `Retry-After`; erros
permanentes aguardam seis horas e outros `4xx`, uma hora. Reiniciar o serviço
após corrigir a configuração não preserva essa espera em memória.

Comandos de controle são persistidos separadamente e só são reconhecidos
depois de o daemon observar o efeito em `LockedHint`. O servidor devolve os IDs
de comando que recebeu para o agente remover esses reconhecimentos locais, sem
retransmiti-los indefinidamente.

Uma sessão nova solicita ao servidor uma âncora com saldo confirmado. A
identidade combina o namespace privado do ciclo do serviço e a sessão logind.
Depois da resposta, o daemon
subtrai somente o uso monotônico posterior; heartbeats sem mudança não reaplicam
saldo. O heartbeat também informa presença gráfica separadamente do estado
online, permitindo que o painel pare o contador quando a sessão gráfica termina.

O contrato e seus estados estão documentados em `docs/synchronization.md` e
`docs/arquitetura-comunicacao.md`. Mantenha a vigilância pausada em qualquer
ensaio integrado para não bloquear a própria sessão de desenvolvimento.
