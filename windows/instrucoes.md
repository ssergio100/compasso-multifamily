O instalador do Compasso já está funcional e instala corretamente a interface de adição de tempo e a tela de configurações, utilizando o layout planejado.

**Importante:** apesar de o instalador estar pronto, as aplicações instaladas atualmente são apenas a **casca da interface**. Elas devem ser aproveitadas como base visual e estrutural da versão Windows, mas ainda não possuem a lógica real do agente.

Não recrie essas interfaces do zero e não substitua a estrutura já pronta sem necessidade. A partir deste ponto, o trabalho consiste principalmente em integrar às aplicações existentes as funcionalidades reais atualmente executadas pelo agente cliente Linux.

A próxima etapa é migrar para Windows as funcionalidades atualmente existentes no agente cliente Linux.

Antes de implementar qualquer funcionalidade, compreenda o contrato existente entre o agente, o servidor e os demais componentes do sistema. Identifique o que o cliente Linux faz atualmente, quais dados recebe, quais dados envia, quais estados mantém e quais ações executa.

Uma limitação já conhecida da versão Windows é o desbloqueio remoto da máquina. Não devemos gastar tempo tentando contornar essa limitação com soluções improvisadas. Quando a máquina estiver bloqueada, o Compasso deve aguardar que o desbloqueio seja realizado manualmente pelo usuário autorizado.

## Objetivo principal

Concluir a portabilidade funcional do agente Linux para Windows, utilizando como base as aplicações e interfaces que já estão prontas, com:

- o menor número possível de alterações desnecessárias;
- o menor consumo possível de tokens;
- soluções simples e convencionais;
- testes pequenos e verificáveis;
- documentação suficiente para que outro agente continue o trabalho sem depender de uma nova explicação do usuário.

A prioridade é concluir o projeto, não explorar alternativas tecnicamente interessantes que não sejam necessárias.

## Antes de programar

Analise o agente Linux atual e produza um documento curto descrevendo:

- responsabilidades do agente;
- comunicação com o servidor;
- comandos recebidos;
- respostas enviadas;
- estados importantes;
- persistência necessária;
- comportamento de inicialização;
- comportamento em caso de perda de comunicação;
- controle de tempo;
- bloqueio da sessão;
- diferenças inevitáveis entre Linux e Windows;
- quais funcionalidades devem ser conectadas à interface de adição de tempo;
- quais funcionalidades devem ser conectadas à aplicação de configurações;
- quais funções devem permanecer em componentes de serviço ou background e não na interface.

Não reescreva o funcionamento com base em suposições. Sempre que possível, use o código existente como especificação do comportamento esperado.

## Plano de implementação

Após compreender o contrato, crie um checklist curto com as etapas realmente necessárias para a migração.

Cada item deve representar algo que possa ser implementado e testado objetivamente.

Evite itens genéricos como:

"Implementar comunicação."

Prefira algo verificável, por exemplo:

"Conectar ao servidor e validar o recebimento de uma mensagem."

Se um item exigir várias ações independentes, divida-o em dois ou mais itens.

O checklist deve permanecer pequeno. Não transforme o projeto inteiro em dezenas de microtarefas.

Após cada etapa validada com sucesso, atualize imediatamente o checklist.

## Uso de tokens

Tokens são um recurso limitado deste projeto.

Antes de executar uma tarefa potencialmente cara, verifique se ela realmente precisa ser feita pelo agente.

Se algo puder ser executado facilmente pelo usuário no terminal, descreva exatamente o comando ou procedimento necessário e peça que ele execute.

Exemplos apropriados para delegação:

- compilar;
- executar um comando diagnóstico;
- verificar um log;
- testar comportamento da interface;
- instalar uma dependência;
- copiar a saída de um comando;
- confirmar o comportamento do Windows.

Não consuma grandes quantidades de tokens simulando, investigando ou reconstruindo algo que possa ser confirmado rapidamente pelo usuário.

## Resolução de problemas

Diante de um erro, procure primeiro a causa mais simples e convencional.

Não crie arquiteturas paralelas, abstrações adicionais, novos frameworks ou soluções alternativas antes de verificar o funcionamento esperado da implementação atual.

Se existir uma correção direta e ortodoxa, utilize-a.

Não transforme um problema pequeno em uma reformulação do projeto.

Quando uma limitação do sistema operacional impedir determinada funcionalidade, informe claramente ao usuário e continue com o comportamento previsto para essa condição.

## Decisões durante o desenvolvimento

Compreenda o propósito do Compasso antes de tomar decisões técnicas.

Quando o contexto do projeto permitir determinar claramente a solução correta, tome a decisão e prossiga.

Não transfira decisões triviais para o usuário apenas para evitar responsabilidade técnica.

Pergunte somente quando existirem alternativas que realmente alterem o comportamento do produto ou quando faltar uma informação impossível de obter no código ou na documentação existente.

Na dúvida entre investigar extensivamente ou utilizar uma solução simples compatível com o projeto, prefira a solução simples.

Na dúvida sobre gastar muitos tokens em uma investigação de baixo impacto, não gaste.

## Continuidade entre sessões

Os tokens disponíveis podem terminar antes da conclusão do projeto.

Por isso, mantenha continuamente um documento de contexto para o próximo agente.

Esse documento deve ser sintético e atualizado, contendo apenas:

- objetivo do projeto;
- arquitetura adotada;
- decisões já tomadas;
- limitações conhecidas;
- funcionalidades concluídas;
- funcionalidade atualmente em desenvolvimento;
- arquivos importantes;
- comandos relevantes;
- problemas ainda abertos;
- próximo passo recomendado.

Deixe explícito nesse documento que o instalador e as interfaces já existem e devem ser reutilizados, mas que as aplicações ainda são apenas a camada visual, sem a implementação completa das funcionalidades do agente.

Não transforme esse documento em um histórico completo da conversa.

Ele deve permitir que outro agente continue o trabalho rapidamente sem exigir que o usuário explique novamente todo o projeto.

## Organização do ambiente

Se encontrar muitos arquivos de testes antigos, implementações abandonadas ou documentação contraditória das tentativas anteriores, identifique o que ainda é válido.

Remova ou separe arquivos claramente obsoletos quando isso puder ser feito com segurança.

Mantenha poucos arquivos de referência e deixe explícito quais representam o estado atual do projeto.

Esta portabilidade já foi tentada anteriormente e falhou principalmente por perda de contexto, decisões inconsistentes e implementação de soluções que não correspondiam ao que o usuário esperava.

Evite repetir esse padrão.

## Regra operacional

O fluxo ideal para cada funcionalidade é:

1. Entender como funciona no agente Linux.
2. Identificar onde essa funcionalidade se encaixa na estrutura Windows já existente.
3. Implementar a solução mais simples, reutilizando a interface atual.
4. Compilar.
5. Testar.
6. Corrigir apenas o que falhou.
7. Marcar o item como concluído.
8. Atualizar o documento de continuidade.
9. Prosseguir para o próximo item.

Não recrie o que já está pronto.

Não antecipe problemas que ainda não ocorreram.

Não refatore código funcional sem necessidade.

Não introduza dependências sem benefício concreto.

Não implemente funcionalidades que ainda não estejam sendo trabalhadas.

O objetivo é transformar a casca já instalada em uma versão funcional do Compasso para Windows no menor tempo possível, com o menor retrabalho e o menor consumo de tokens possível.