# Sincronização agente–servidor

O servidor é a autoridade sobre configuração, bônus remoto e controles
administrativos. O agente é a autoridade sobre o que efetivamente ocorreu na
máquina: presença da sessão, tempo monotônico decorrido e efeito de um bloqueio.

## Ciclo principal

1. O agente envia heartbeat autenticado com data local, revisões aplicadas,
   consumo acumulado, presença da sessão, eventos e confirmações pendentes.
2. O servidor persiste os dados de forma idempotente e responde com alterações
   relevantes, comandos e o próximo intervalo de heartbeat.
3. Ao iniciar uma sessão ou receber uma revisão relevante, o agente persiste
   uma âncora de saldo confirmada pelo servidor.
4. Entre heartbeats, o agente desconta apenas o tempo monotônico posterior à
   âncora e continua avaliando rotinas localmente.
5. Uma operação remota só aparece como concluída depois que o agente persistir
   ou observar seu efeito e devolver a confirmação.

## Responsabilidades

| Componente | Responsabilidade |
| --- | --- |
| servidor | calcular saldo confirmado, versionar política e guardar operações |
| agente | medir tempo, aplicar rotinas/bloqueios e confirmar efeitos reais |
| SQLite local | manter âncora, revisões, eventos e confirmações entre reinícios |
| painel | apresentar o estado do servidor e solicitar mudanças |

Agente online e sessão gráfica ativa são estados diferentes. O painel só anima
o contador quando a sessão está presente e o agente informa que está contando.

## Compatibilidade e falhas

- o heartbeat anuncia `X-Compasso-Protocol-Version: 2`;
- cada instalação anuncia a capacidade `installation-identity` e envia seu
  UUID v4 persistido; outro UUID para a mesma credencial recebe
  `409 installation_conflict`;
- campos opcionais ausentes usam valores seguros e o intervalo padrão de três
  segundos;
- `401 invalid_device_credentials`, `403 family_suspended`,
  `409 installation_conflict` e `409 revision_ahead` são erros permanentes;
- `426 agent_upgrade_required` indica que o agente precisa ser atualizado;
- `429 rate_limited` respeita `Retry-After` entre cinco segundos e uma hora;
- falhas de rede e `5xx` usam backoff exponencial de um segundo até cinco
  minutos; outro `4xx` é tratado como incompatibilidade;
- uma falha de rede não apaga a última autorização válida nem confirma um
  comando;
- eventos e comandos possuem identificadores duráveis para tolerar reenvios.

Com a identidade negociada, o servidor responde com 5 segundos durante uma
sessão gráfica e 30 segundos sem sessão e calcula `online_until` usando no
mínimo 60 segundos ou quatro vezes o intervalo. O agente acrescenta jitter de
até 10% ao próximo ciclo normal.

Detalhes de payloads, endpoints, revisões e estados estão em
[arquitetura-comunicacao.md](arquitetura-comunicacao.md). Exemplos executáveis
estão na [coleção Postman](Compasso_API.postman_collection.json).
