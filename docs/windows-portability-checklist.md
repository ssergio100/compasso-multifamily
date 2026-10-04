# Windows — checklist de portabilidade

## Portabilidade funcional vigente

- [x] Identificar a interface vigente e documentar o contrato Linux, servidor,
      diferenças Windows e divisão entre interface e serviço.
- [x] Criar o executável de serviço Windows e validar instalação, início,
      parada e persistência de um estado mínimo em `%ProgramData%\Compasso`.
- [x] Substituir `loginctl` por detecção/bloqueio de sessão Windows e validar
      conta local, sessão remota ignorada, sessão bloqueada sem consumo e
      retomada da contagem somente após desbloqueio manual.
      - Detecção por SID e classificação de bloqueio validadas
      - `WTSDisconnectSession` da Session 0 retorna sucesso e desconecta a sessão
        console sem encerrar aplicativos; o adaptador `session.Windows` reporta a
        sessão como bloqueada, que é a condição que impede a contagem de tempo.
        Evidência: execução direta do método `session.Windows.Lock` a partir da
        Session 0, com observação do estado por `Sessions`.
      - `LockWorkStation()` da Session 0 retorna `ERROR_ACCESS_DENIED`, portanto
        é impróprio para o serviço. O código original é correto.
      - Ressalva: rejeição de sessão RDP apenas por teste unitário.
- [x] Reutilizar SQLite, política e sincronização; validar um heartbeat, uma
      política aplicada, um comando confirmado e recuperação após ficar offline.
      - Fiação do serviço validada: config DPAPI → store → `BindEnrollment` →
        `session.NewWindows` → `daemon.New` → `syncclient` → `localauth`.
        Intervalos espelham o Linux (tick 1s, checkpoint 5s, HTTP 8s,
        heartbeat 3s). `Notifier` no-op explícito; o daemon tolera `nil`.
      - Token gravado por `configure` com DPAPI e ACL, lido de stdin para não
        aparecer na linha de comando. Segredo não recuperável pela interface.
      - **Heartbeat validado** em `https://apifamily.smresume.com`: o agente
        reportou `synchronization online`, confirmando TLS, cabeçalhos,
        identidade de instalação e aceitação do dispositivo pelo servidor.
      - **Política recebida e aplicada**: revisão 7 aplicada pelo servidor e
        confirmada pelo agente, com `weekly_quota` por dia da semana e
        `warning_minutes=10`.
      - **Item 4 concluído.** Cenários validados pelo usuário na máquina real:
        bloquear ao esgotar o tempo; retomar ao adicionar tempo; bloqueio
        manual; desbloqueio manual com senha na sessão; pausa da monitoração;
        pausa sem contagem de tempo.
- [ ] Expor IPC local protegido e conectar **Adicionar tempo**; validar de ponta
      a ponta pela interface vigente `windows/CompassoWails` o status do serviço,
      a escolha do período, a senha correta/incorreta, o rate limit, o serviço
      indisponível, a falha interna e o evento durável.
      - Implementação concluída no serviço, IPC, interface Wails e instalador;
        não há lacuna funcional conhecida no item 5.
      - A interface instalada já cobriu estado do serviço, escolha do período,
        senha incorreta, rate limit, serviço indisponível e limpeza da senha.
      - Única pendência: concluir os testes de aceite pela interface instalada,
        incluindo o sucesso com senha real e a conferência do bônus, evento e
        saldo persistidos. Essa pendência não bloqueia o início do item 6.
- [x] Conectar **Configurações** ao caminho privilegiado; validar conta, URL,
      credenciais, primeiro heartbeat e segredo não recuperável pela interface.
      - A implementação vigente é exclusivamente `windows/CompassoWails`; a
        WinUI histórica não foi alterada.
      - Conta local e SID são enumerados pela interface e revalidados pelo
        helper elevado; a confirmação é obrigatória e volta a ficar desmarcada
        ao trocar a conta.
      - URL, dispositivo e token são validados antes da gravação; o token cru
        cruza somente um pipe efêmero restrito a administradores e é persistido
        com DPAPI, sem retorno pela interface.
      - O serviço só habilita a política depois do primeiro heartbeat aceito e
        da gravação durável de `setup-complete`. Sucesso, credencial rejeitada,
        estado do serviço, reinício e token limpo foram cobertos na VM.
- [ ] Incluir serviço e interface no instalador vigente e validar do zero:
      instalar, configurar, reiniciar, atualizar, desinstalar e não deixar estado
      executável órfão.
      - Serviço e interface já fazem parte do `CompassoSetup.exe`; ciclo de
        desinstalação/reinstalação, remoção do serviço e atualização com a
        interface aberta passaram na VM.
      - A primeira fase do aceite realmente limpo passou: instalação sem
        `%ProgramData%` preexistente, payload idêntico à build, atalhos e
        desinstalador presentes, nenhuma configuração ou marcador herdado e
        serviço `Manual`/`Stopped` até a primeira configuração, como no Linux.
      - O uninstall agora encerra e aguarda `Compasso.exe`, remove
        `%ProgramData%\Compasso` e não deixa o executável bloqueado. Na VM,
        instalação por cima preservou a configuração existente e instalação
        após uninstall completo voltou a `Manual`/`Stopped`, sem configuração
        nem `setup-complete`.
      - Permanece pendente concluir o aceite integral da portabilidade pela
        tela e com reinício do Windows; isso não faz parte desta correção.

Contrato: [windows-agent-contract.md](windows-agent-contract.md).

## Base visual entregue — instalador enxuto da interface

- [x] Um único artefato distribuível: `CompassoSetup.exe`.
- [x] Instalador com 5,40 MiB, sem .NET ou Windows App SDK embutidos.
- [x] Instalação limpa, desinstalação e reinstalação validadas.
- [x] Atalhos do menu Iniciar e da área de trabalho validados.
- [x] Aplicação instalada inicia e permanece ativa.
- [x] Assistente gráfico percorrido e ícone da raposa validado no instalador,
      atalho, janela e barra de tarefas.
- [x] Microsoft Defender não encontrou ameaças no artefato.
- [ ] Assinatura digital de distribuição; depende de certificado e não bloqueia
      o uso interno do instalador.

## Histórico WinUI — fora do escopo vigente

- [x] Ambiente Windows e template oficial WinUI verificados.
- [x] Primeiro protótipo nativo do **instalador** compila e abre no Windows.
- [x] Correspondência visual do protótipo com o desenho aprovada em revisão.
- [x] Fluxo do instalador, elevação administrativa e tratamento de falhas validados.
      Quatro caminhos exercitados na interface — Confirmado, Cancelado, Falha e
      Já elevado — mais os estados de preparação, sob UAC no padrão do Windows.
      Cinco sub-ramos defensivos sem cobertura ficam registrados no log e
      voltam junto do quinto item.
- [ ] Instalador implanta o serviço e as interfaces para todas as contas.
      A interface real foi instalada e atualizada em `C:\Program Files\Compasso`,
      para todas as contas, com atalho e registro de desinstalação. O serviço
      ainda não faz parte do payload e mantém este item aberto.
- [x] Interfaces nativas **Adicionar tempo** e **Configurações** implementadas.
- [x] Layout das interfaces comparado visualmente e alinhado aos conceitos em
      `docs/design/windows-shell/`; capturas finais reproduzíveis em
      `docs/design/windows-shell/validation/`. As exceções intencionais estão
      explicitadas no log. Campos de texto e senha das duas interfaces usam
      contorno uniforme nos quatro lados, sem a borda inferior do template
      Fluent padrão, inclusive durante o foco.
- [ ] Integração funcional das interfaces com o serviço real.
      O serviço ainda não está instalado; as telas informam isso sem simular
      sucesso.
- [ ] Ação local **Suspender/Reativar bloqueios** validada.
- [ ] Serviço e interfaces integrados e testados no Windows.
- [ ] Instalação, atualização e desinstalação validadas do zero.
- [x] Atualização da interface instalada validada sem desinstalação manual;
      troca transacional de diretórios e restauração da versão anterior em caso
      de falha implementadas.
- [x] Desinstalar → reinstalar → fechar validado na mesma sessão do instalador.
      Resultado reproduzível: `UNINSTALL=PASS`, `REINSTALL_SAME_SESSION=PASS`,
      fechamento em 529 ms.
- [x] Build encadeada: compilar `CompassoApp` ou `CompassoInstaller` recompõe o
      instalador e o payload; SHA-256 de cada um dos 535 arquivos da árvore do
      app é conferido contra sua cópia empacotada por `windows/build-windows.ps1`.
- [x] Ícone da raposa confirmado visualmente na busca e na barra de tarefas da
      aplicação aberta. O cabeçalho mantém a marca original do Compasso.
- [x] Opção de criar atalho na área de trabalho implementada e validada para
      todas as contas. Marcar cria `C:\Users\Public\Desktop\Compasso.lnk`,
      desmarcar numa atualização remove e marcar novamente recria; a
      desinstalação também remove o atalho.
- [x] Estados interativos dos períodos validados: repouso claro com texto
      escuro; somente o selecionado fica coral com texto branco e permanece
      assim sob hover, clique e foco. A troca exclusiva também foi exercitada
      por clique real no fluxo `30 → 60 → 15`: em cada etapa apenas a nova
      opção ficou selecionada e a ação principal acompanhou o período. Os
      quatro controles foram conferidos inteiros, com borda fechada e quatro
      cantos arredondados, sem recorte lateral, e os quatro rótulos foram
      conferidos centralizados horizontal e verticalmente.

Um item só é concluído com evidência reproduzível registrada no log.
