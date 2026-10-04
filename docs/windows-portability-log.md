# Windows — log de portabilidade

Este é o ponto único de retomada do trabalho. Registre aqui apenas fatos
verificados, decisões vigentes e o próximo passo concreto.

## Estado atual — 2026-10-01

- **Heartbeat real validado.** O serviço Windows agora executa a fiação completa
  do agente e o servidor respondeu bem. Detalhes e próximos passos ao final
  desta seção.
- A interface **vigente** é `windows/CompassoWails`; `CompassoApp` e
  `CompassoInstaller` são históricos. Ao retomar, ler
  `docs/windows-agent-contract.md` e o checklist antes de tocar em qualquer
  interface.

## Estado anterior — 2026-09-30

- O escopo vigente agora é concluir a portabilidade funcional do agente Linux
  para Windows, preservando o instalador e as interfaces já aprovadas.
- A implementação de interface vigente é `windows/CompassoWails`, empacotada
  por `windows/build-portable-installer.ps1` com Inno Setup.
- `CompassoApp` e `CompassoInstaller` passam a ser históricos e não devem ser
  compilados ou distribuídos: os dois projetos WinUI eram self-contained e
  duplicavam .NET e Windows App SDK no pacote.
- O contrato levantado diretamente de `agent/`, `protocol/v1/` e do servidor
  está em `docs/windows-agent-contract.md`. Ele registra responsabilidades,
  heartbeat, comandos, persistência, inicialização, falhas, tempo, bloqueio e
  o encaixe das duas telas.
- Arquitetura adotada: serviço Windows separado para política, SQLite,
  sincronização, tempo e sessão; `CompassoWails` permanece como interface e
  usa IPC local protegido. Pacotes Go independentes de Linux serão reutilizados.
- Limitação confirmada: o Windows não será desbloqueado remotamente. Depois de
  suspender ou limpar um bloqueio, o serviço aguardará o desbloqueio manual
  antes de confirmar o efeito ao servidor.
- Decisão de produto confirmada pelo usuário: uma sessão Windows bloqueada
  jamais consome tempo. A contagem para ao detectar o bloqueio e só volta após
  o desbloqueio manual. A regra foi aplicada ao motor compartilhado e o README
  do agente Linux foi corrigido para refletir o novo comportamento.
- Funcionalidade em desenvolvimento: criar o executável mínimo do serviço
  Windows, com ciclo de vida e persistência em `%ProgramData%\Compasso`.
- O primeiro esqueleto foi implementado em
  `agent/cmd/compasso-agent-windows` e `agent/windowsservice`: registro
  idempotente no Service Control Manager, início automático atrasado,
  recuperação após falha, início/parada, modo console e estado atômico sem
  segredos em `%ProgramData%\Compasso\service-state.json`.
- `windows/build-agent-service.ps1` compila o executável e
  `windows/verify-agent-service.ps1` valida instalar, iniciar, persistir
  `running`, parar, persistir `stopped`, reiniciar e remover. O verificador se
  recusa a alterar um serviço `CompassoAgent` preexistente.
- Testes locais de `agent/windowsservice` passaram. A compilação Windows não
  podia ser feita neste Linux porque o único compilador disponível é `gccgo`,
  que não suporta o backend Windows de `x/sys`.
- A máquina Windows voltou a ficar acessível por SSH. Os arquivos mínimos foram
  espelhados em `C:\CompassoWinUIBootstrap\agent-port`, sem alterar a instalação
  vigente. Build com Go 1.27.0 concluída: `CompassoAgent.exe` com 4.625.920
  bytes e SHA-256
  `F1FB07DC874E8866780C16296E749312D269927D919510DADDE4883F9A7AD502`.
- Ciclo real do Service Control Manager validado em 30/09/2026:
  `INSTALL=PASS`, `START=PASS`, `RUNNING_STATE=PASS`, `STOP=PASS`,
  `STOPPED_STATE=PASS`, `RESTART=PASS` e `UNINSTALL=PASS`. O verificador removeu
  o serviço de teste ao final; a instalação vigente não foi tocada.
- O segundo item do checklist está concluído.
- Funcionalidade agora em desenvolvimento: reutilizar o motor independente de
  plataforma (SQLite, política, sincronização) dentro do serviço Windows, para o
  quarto item. O adaptador de sessão foi concluído e validado (ver correções de
  2026-09-30 mais adiante nesta seção).
- Adaptador WTS implementado em `agent/session/windows.go`. A classificação
  aceita somente protocolo de console local, compara a conta pelo SID, lê o
  estado de bloqueio e trata estado desconhecido como bloqueado. Sessões RDP são
  ignoradas. `Unlock` não tenta contornar o Windows e apenas aguarda o usuário.
- Observação real validada na máquina: SID
  `S-1-5-21-278194532-2139705530-887251162-1002`, sessão `1`, usuário
  `windows11\Sergio`, `Remote=false` e `Locked=false`.
- O motor compartilhado foi alterado para nunca consumir tempo durante o
  bloqueio. O teste cobre bloqueio, permanência bloqueada, desbloqueio sem cobrar
  o intervalo anterior e retomada posterior; todos os testes de agente e
  protocolo passam.
- Build Windows posterior do adaptador passou com 4.976.128 bytes e SHA-256
  `02A0282C98903DFA08AB2C1DA3CD92D3C2A7060579A288D9CB7CA7DDB23CD7AC`.
- Pendência para concluir o terceiro item: executar o bloqueio real quando o
  usuário puder desbloquear manualmente e confirmar a transição de estado.
- **Correção 2026-09-30 — o bloqueio de CGO descrito acima não existe.** O
  MSYS2 e o `mingw-w64-ucrt-x86_64-gcc` já estavam instalados em
  `C:\msys64\ucrt64\bin\gcc.exe` (gcc 16.2.0). O que não existia era o `gcc` no
  `PATH`; `windows/build-agent-service.ps1` já referenciava o caminho absoluto
  e por isso nunca precisou de instalação nova. Nenhum produto foi instalado
  nesta etapa.
- CGO e SQLite comprovados no Windows: com `CGO_ENABLED=1` e `CC` apontando
  para o gcc do MSYS2, `agent/storage`, `agent/policy`, `agent/syncclient`,
  `agent/localauth`, `agent/syncstatus` e `protocol/v1` compilam, e os testes
  desses pacotes passam na máquina Windows, incluindo `agent/storage`, que
  executa SQLite de verdade (`ok ... 1.048s`). **O quarto item não tem mais
  bloqueio de build.**
- Defeito corrigido: `agent/windowsservice/config_windows.go` usava `clear()`,
  que exige Go 1.21, mas o `go.mod` do módulo declara `go 1.18`. A build do
  serviço falhava com `clear requires go1.21 or later`. O `go.mod` **não** foi
  alterado, para não impor Go novo ao agente Linux por causa de um arquivo
  Windows-only; `clear(token)` foi trocado por `zeroBytes`, um helper local de
  três linhas no mesmo arquivo. `clear()` aparecia uma única vez no repositório.
- Defeito corrigido: `agent/session/loginctl_test.go` não tinha restrição de
  plataforma, ao contrário de `loginctl.go`, que tem `//go:build !windows`. No
  Windows o pacote `session` não compilava para teste, por referenciar
  `newLogind`, `parseProperties` e `readSessionNamespace`, que só existem no
  caminho Linux. A mesma restrição foi adicionada ao teste.
- Após as duas correções: `gofmt` limpo, testes Linux de `agent/...` e
  `protocol/...` passando, e testes Windows de `storage`, `session`, `policy`,
  `syncclient`, `localauth`, `windowsservice` e `protocol/v1` com `EXIT=0`.
- Build do serviço refeita no Windows: `CompassoAgent.exe` com 4.974.592 bytes e
  SHA-256
  `9A19B389F08584DD7F0CC698E024475C2D030B2E367E39A261A903E9B12F2184`.
- Ressalva sobre a build acima: `agent/cmd/compasso-agent-windows` **não importa
  nenhum pacote do repositório**. Ela valida o esqueleto do SCM, e não o motor.
  Uma build verde desse alvo não prova nada sobre CGO; a prova está nos testes
  de `agent/storage` citados acima.
- **Primitiva de bloqueio corrigida.** `agent/session/windows.go` usava
  `WTSDisconnectSession` para "travar" a sessão. Essa API é de sessões RDP e, na
  sessão console, desconecta em vez de bloquear. Foi trocada por
  `user32!LockWorkStation`, que trava a tela sem encerrar aplicativos nem
  desconectar a sessão — o comportamento exigido pelo contrato. `Unlock`
  continua sendo no-op: o Windows exige destrave manual do usuário autorizado.
- **Terceiro item validado na máquina real.** `windows/verify-session-lock.ps1`
  compila um harness (`windows/verify-session-lock/`) que usa o adaptador de
  sessão de verdade, agenda-o na sessão gráfica interativa e observa o ciclo
  completo. Saída reproduzível em 30/09/2026:

  ```text
  BEFORE=id=1 user=windows11\Sergio remote=false locked=false
  LOCK_CALLED=LockWorkStation returned success
  LOCK_OBSERVED=id=1 locked=true after lock
  CHARGE_GUARD_OK=session stayed locked for 10s, no charge permitted
  WAITING_UNLOCK=...
  UNLOCK_OBSERVED=id=1 locked=false after manual unlock
  RESULT=PASS
  ```

  Isso cobre conta local pelo SID, bloqueio real preservando a sessão, janela
  sem consumo enquanto bloqueada e retomada somente após destrave manual.
- Ressalva do terceiro item: a rejeição de sessão remota está coberta por teste
  unitário (`TestClassifyWindowsSession/rdp_ignored`) e pela classificação de
  protocolo, mas não foi exercitada com uma sessão RDP real.
- **Revertido em 2026-09-30 — `Lock()` volta a `WTSDisconnectSession`.** Durante
  esta sessão, `Lock` foi trocado por `user32!LockWorkStation` com base em
  suposição, sem medição. A medição seguinte derrubou a própria troca:
  `LockWorkStation()` chamada da Session 0 retorna `False` com `LAST_ERROR=5`
  (`ERROR_ACCESS_DENIED`) e a sessão console continua `Ativo`. `LockWorkStation`
  só funciona de dentro da sessão gráfica do usuário, que é exatamente onde o
  serviço **não** roda. O código original com `WTSDisconnectSession` está
  correto e é o que fica. A troca foi desfeita com `git checkout`, e
  `agent/session/windows.go` está idêntico ao estado anterior.
- Lição registrada: `WTSDisconnectSession` é a API que a Session 0 consegue
  controlar, e foi a escolha certa desde o início. `LockWorkStation` é
  semanticamente mais fiel ao `Win+L`, mas exige um processo na sessão do
  usuário, o que exigiria um componente adicional. Não trocar código funcional
  sem antes medir a restrição real.
- **Bloqueio validado a partir da Session 0 com o caminho real do produto.**
  Executando `session.Windows.Lock` — o mesmo código do serviço — de um processo
  na Session 0, na conta `windows11\Sergio`, SID
  `S-1-5-21-278194532-2139705530-887251162-1002`, sessão `1`:

  ```text
  MATCHED_BEFORE: 1
    id=1 user=windows11\Sergio remote=false locked=false
  LOCK_RETURNED_NIL after 317ms
    t+02s id=1 locked=true
    ... locked=true até t+16s
  ```

  E `query user` passou a reportar a sessão `1` como `Disco` (desconectada).
  Ou seja: `WTSDisconnectSession` **funciona da Session 0**, desliga a sessão
  sem encerrar aplicativos, e o próprio adaptador passa a reportá-la como
  bloqueada — que é a condição que impede a contagem de tempo.
- Comparação que fecha a questão: `LockWorkStation()` chamada da Session 0
  retorna `False` com `LAST_ERROR=5` (`ERROR_ACCESS_DENIED`) e não trava nada.
  `WTSDisconnectSession` da Session 0 retorna sucesso e trava. Para um serviço
  Windows, a escolha é `WTSDisconnectSession`, e o código original está correto.
- Corpo do terceiro item validado. Ressalva que permanece: a rejeição de sessão
  remota real (RDP) está coberta por teste unitário e pela classificação de
  protocolo, não por uma sessão RDP exercitada.
- O harness `windows/verify-session-lock/` e o script que o agendava foram
  removidos. Ele foi escrito para o contexto da sessão gráfica, que não é o
  contexto do serviço, e por isso não serve como evidência do bloqueio. A
  evidência do serviço é a execução direta do `session.Windows.Lock` da Session
  0 registrada acima. O terceiro item não depende de nenhum harness.

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\windows\build-agent-service.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\windows\verify-agent-service.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\windows\verify-session-lock.ps1
```

## Entrega visual concluída — 2026-09-30

- O escopo anterior era um único instalador `.exe` enxuto que instalava somente
  a interface Windows; essa etapa visual foi concluída e agora serve de base.
- `CompassoSetup.exe` foi reconstruído no Windows em 30/09/2026 com 5.660.559
  bytes (5,40 MiB) e SHA-256
  `508A3485B7CC0FB0CE0DDD88260CE365E6102C6E854280F042500A7880575DD8`.
- O ciclo reproduzível partindo de instalação limpa passou: instalar,
  desinstalar, reinstalar, recriar atalhos e deixar apenas três arquivos em
  `C:\Program Files\Compasso` — `Compasso.exe`, o ícone versionado e os dois
  arquivos necessários do desinstalador. Nenhuma DLL ou arquivo de runtime
  .NET foi instalado.
- O assistente foi percorrido na sessão gráfica e a aplicação instalada abriu
  responsiva ao concluir. O ícone da raposa foi confirmado no instalador, no
  atalho do desktop, na janela e na barra de tarefas. O Microsoft Defender
  examinou o instalador e não encontrou ameaças.
- O artefato ainda não tem assinatura digital de distribuição.

## Estado anterior — 2026-09-29

- A elevação administrativa do instalador está implementada, corrigida e
  validada por evidência reproduzível sob UAC no padrão do Windows.
- Os quatro caminhos de elevação foram exercitados de ponta a ponta na
  interface: **Confirmado**, **Cancelado**, **Falha** e **Já elevado**. Os
  estados de preparação também foram observados com o prompt aberto. O quarto
  item do checklist está concluído, com sub-ramos defensivos registrados como
  cobertura conhecida.
- O instalador é um produto real, não uma maquete: será um `.exe` que instala a
  interface **Adicionar tempo**, que por sua vez chama **Configurações**, cria
  ícone e permite desinstalar. Ver [Escopo do instalador](#escopo-do-instalador).
- Primeira implementação vigente: `CompassoInstaller`, um instalador nativo em
  WinUI 3/C#. O instalador deve existir e ser validado antes das demais
  interfaces Windows.
- A sessão SSH na máquina de testes já é elevada. A validação de elevação
  precisa obrigatoriamente de um processo **não elevado** na sessão
  interativa `1`, por exemplo `schtasks /IT /RL LIMITED`.
- O instalador será responsável por implantar o serviço e as interfaces
  `CompassoWindows` para todas as contas do computador.
- Modelo de implantação das interfaces instaladas: **unpackaged**, para
  execução direta e instalação pelo `CompassoInstaller`.
- Interfaces web, Wails, Electron e tentativas Windows anteriores estão fora do
  escopo.
- Os conceitos visuais aprovados estão em `docs/design/windows-shell/`.
- A máquina de desenvolvimento e teste é `Sergio@192.168.122.74` (hostname
  `windows11`, x64).
- Em 2026-09-29 a máquina tem .NET SDK 10.0.401, template oficial `winui`,
  Developer Mode e Visual Studio Community 2026 com os workloads necessários.
- Existe em `C:\CompassoWinUIBootstrap\CompassoInstaller` um scaffold WinUI
  gerado em 2026-09-28. Ele compila, mas sua `MainPage.xaml` está vazia e ele
  foi preservado somente como referência antiga.
- O protótipo vigente está versionado em `windows/CompassoInstaller` e é
  espelhado para testes em
  `C:\CompassoWinUIBootstrap\unpackaged\CompassoInstaller`.
- A reversão integral da tentativa Windows anterior foi commitada junto com o
  primeiro protótipo, em `e7cd265`. A árvore de trabalho está limpa e não há
  alterações locais pendentes a preservar.
- A sessão SSH na máquina de testes já é elevada. A validação de elevação
  precisa obrigatoriamente de um processo **não elevado** na sessão
  interativa `1`, por exemplo `schtasks /IT /RL LIMITED`.

## Escopo histórico do instalador WinUI

O `CompassoInstaller` é um produto real, não uma maquete de navegação. O que
precisa existir e funcionar:

- um `.exe` instalador de verdade, não um protótipo de interface;
- instalação da interface **Adicionar tempo**;
- a interface **Adicionar tempo** chama a interface **Configurações**;
- criação de ícone para as interfaces instaladas;
- desinstalação disponível.

A elevação é tratada exatamente como qualquer instalador trata: pedido padrão
do Windows por `Verb = "runas"`, sem contorno, sem elevação silenciosa e sem
atalho de teste. O que já foi validado sob UAC no padrão é o comportamento que
servirá de contrato.

**Verificar elevação não é instalar.** Hoje o instalador confirma que a
permissão foi concedida e encerra. O processo elevado ainda precisa executar o
trabalho real — copiar os componentes, criar o ícone e registrar a
desinstalação. Como isso será feito (comando pelo pipe nomeado, ou o processo
elevado assumindo a instalação) é decisão de projeto do quinto item do
checklist e ainda não foi tomada.

## Decisões históricas da tentativa WinUI

As decisões desta seção foram preservadas como evidência, mas foram
substituídas pelo estado atual no início do documento. Não devem orientar a
portabilidade funcional vigente.

- A validação de comportamento no Windows só vale sob
  `ConsentPromptBehaviorAdmin=5`, que é o padrão do Windows. O modo
  `ConsentPromptBehaviorAdmin=0` é conveniência da máquina de teste e não
  representa o usuário real; nele a elevação é instantânea e esconde falhas de
  interface. Esse é o único desvio de UAC tolerado, e apenas para acelerar
  teste, nunca como evidência de comportamento.
- Interface: WinUI 3 e Windows App SDK, usando controles XAML nativos.
- Aparência: Fluent moderno, mantendo marca, hierarquia e composição dos
  conceitos aprovados; não usar o assistente MSI clássico como interface final.
- Ícones: Segoe Fluent Icons; não usar emojis como ícones.
- Ordem obrigatória de implementação: primeiro o **instalador**; depois o
  serviço e as interfaces que ele instalará. A tela **Adicionar tempo** não é o
  primeiro protótipo.
- Primeiro protótipo: tela inicial do `CompassoInstaller`, ainda sem executar
  mudanças reais no sistema.
- Depois do instalador, a primeira interface instalada será **Adicionar
  tempo**, incluindo a representação do estado **Bloqueios suspensos**.
- Comportamento futuro de emergência: suspender somente a emissão/execução do
  bloqueio; serviço, contagem e sincronização continuam funcionando.
- O estado local será chamado **Suspender bloqueios** / **Reativar bloqueios**.

## Evidências e comandos

### Auditoria inicial da máquina

Executado remotamente:

```powershell
dotnet --info
winget --version
dotnet new list winui
```

Resultado: `dotnet` ausente; WinGet `v1.29.380`; template WinUI indisponível.

### Correção de escopo — 2026-09-29

Foi confirmado que o instalador é o primeiro produto Windows a ser
implementado. Ele instalará posteriormente o serviço e as interfaces. O
scaffold `CompassoInstaller` existente na máquina foi inspecionado: há projeto,
binário de Debug e janela WinUI padrão, porém `MainPage.xaml` contém apenas um
`Grid` vazio. Portanto, ainda não há interface de instalador testável.

### Preparação do ambiente — 2026-09-29

Executado novamente na máquina Windows, a partir de
`C:\CompassoWinUIBootstrap`:

```powershell
winget configure -f config.yaml --accept-configuration-agreements --disable-interactivity
dotnet --info
dotnet new list winui
```

Resultado verificado: configuração aplicada com êxito; .NET SDK 10.0.401
presente; template oficial `WinUI Blank App` disponível. O primeiro item do
checklist foi concluído.

### Scaffold do instalador — 2026-09-29

O template instalado não oferece a opção `--unpackaged`; `dotnet new winui -h`
o descreve como um template com MSIX. Foi criado um scaffold novo, sem
sobrescrever o anterior, e aplicada a conversão oficial para execução direta:

```xml
<WindowsPackageType>None</WindowsPackageType>
<WindowsAppSDKSelfContained>true</WindowsAppSDKSelfContained>
```

Assim, o `CompassoInstaller` vigente é **unpackaged** e self-contained. Essa
escolha é necessária porque o instalador precisa abrir antes de qualquer
runtime ou interface Compasso estar instalado no computador.

### Primeiro protótipo testável do instalador — 2026-09-29

Implementada em `windows/CompassoInstaller` a tela inicial baseada em
`docs/design/windows-shell/installer-concept.png`, com:

- janela WinUI com Mica e título **Compasso – Instalador**;
- composição adaptativa com painel de marca, título, benefícios e escopo para
  todas as contas;
- recursos de tema claro, escuro e alto contraste;
- botão **Instalar**, que neste protótipo apenas informa que nenhuma alteração
  foi feita;
- botão **Cancelar**, que encerra o aplicativo.

Build executado na máquina Windows:

```powershell
dotnet build .\CompassoInstaller.csproj -c Debug -p:Platform=x64
```

Resultado: compilação concluída com zero erros e zero avisos. O executável foi
iniciado na sessão interativa `1` e permaneceu responsivo. Uma sonda executada
na mesma sessão confirmou uma janela visível com o título
`Compasso - Instalador`, posição `78,78`, tamanho `1044x720`, e uma árvore de
acessibilidade contendo todos os textos esperados e os botões **Instalar** e
**Cancelar instalação** habilitados. A janela foi deixada aberta para revisão.

O segundo item do checklist foi concluído. A aprovação de correspondência
visual permanece pendente até a revisão do usuário.

### Aprovação visual — 2026-09-29

O usuário aprovou o prosseguimento após receber o primeiro protótipo testável.
A correspondência visual foi considerada aprovada e o terceiro item do
checklist foi concluído.

### Fluxo de elevação — 2026-09-29

O quarto item do checklist foi trabalhado parcialmente. A implementação de
elevação já existia em `windows/CompassoInstaller/ElevationVerifier.cs`; o que
faltava era a evidência.

#### Ambiente verificado

```powershell
Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System' `
  -Name EnableLUA, ConsentPromptBehaviorAdmin, PromptOnSecureDesktop
```

Resultado: `EnableLUA=1`, `ConsentPromptBehaviorAdmin=0` e
`PromptOnSecureDesktop=0`, isto é, UAC reduzido ao máximo e elevação silenciosa,
como configurado pelo usuário. A sessão SSH é elevada
(`BUILTIN\Administradores` presente no token), o que impede exercitar a
elevação a partir dela.

#### Compilação

```powershell
dotnet build .\CompassoInstaller.csproj -c Debug -p:Platform=x64
```

Resultado: compilação concluída com zero erros e zero avisos.

#### Direção do pipe nomeado

O servidor do pipe e a sonda precisam ser testados nos dois papéis, porque a
produção usa servidor não elevado e cliente elevado.

| Cenário | Servidor | Sonda | Resposta | Resultado |
| --- | --- | --- | --- | --- |
| A | não elevada | elevada | `elevated` | aprovado |
| B | não elevada | não elevada | `not-elevated` | aprovado |
| C | elevada | não elevada | sem conexão | reprovado |

O cenário C falha com `UnauthorizedAccessException`, porque
`PipeOptions.CurrentUserOnly` não aceita um cliente não elevado para um servidor
elevado. **Esse caminho é inalcançável em produção**: quando o instalador já
sobe elevado, `ElevationVerifier.RequestAsync` retorna no teste
`IsProcessElevated()` e nenhum pipe é criado. A restrição é do
`CurrentUserOnly` e deve ser lembrada caso a estratégia de pipe mude.

#### Rejeição da sonda

Cinco casos foram recusados sem abrir conexão: argumento isolado sem nome de
pipe, pipe sem o prefixo `compasso-installer-`, token que não é GUID, GUID em
formato com hifens e argumento extra no fim. A validação de formato do token em
`TryGetProbePipeName` funciona como pretendido.

#### Fluxo completo — permissão confirmada

O instalador foi iniciado **não elevado** na sessão `1` por
`schtasks /IT /RL LIMITED` e o botão **Instalar** foi acionado por UI
Automation. Com `ConsentPromptBehaviorAdmin=0` a elevação ocorre sem prompt.

Árvore de acessibilidade lida depois da ação:

```text
Ícone de Êxito
Permissão confirmada
Permissão confirmada. O instalador está pronto para receber os componentes nas próximas etapas.
INSTALL_ENABLED_AFTER=True
INSTALL_LABEL_AFTER=Verificar novamente
CANCEL_BUTTON_ENABLED_AFTER=True
```

O `InfoBar` assume severidade de sucesso, os dois botões voltam a ficar
habilitados e o botão primário passa a dizer **Verificar novamente**.

#### Fluxo completo — permissão cancelada

Para exercitar o caminho de cancelamento, o UAC foi colocado temporariamente em
`ConsentPromptBehaviorAdmin=5` e o diálogo foi dispensado pelo usuário
clicando em **Não**. A leitura da interface registrou:

```text
Ícone de aviso
Permissão cancelada
A permissão foi cancelada. Nada foi alterado no computador.
INSTALL_ENABLED_AFTER=True
INSTALL_LABEL_AFTER=Instalar
```

O caminho `Win32Exception` com código `1223` foi reconhecido, o `InfoBar`
assume severidade de aviso e o rótulo do botão primário permanece **Instalar**,
o que é o comportamento correto.

Uma observação de implementação: a janela do diálogo de consentimento tem
classe `Credential Dialog Xaml Host` e **título vazio**, e roda com integridade
alta. Ela não pode ser localizada por título/classe `#32770` nem acionada por
UI Automation vinda de um processo de integridade média. A detecção e a
dispensa precisam ocorrer por nome de processo (`consent`), e a dispensa
programática é bloqueada pelo UIPI.

#### Defeito encontrado — estados de preparação não renderizavam

A validação acima expôs um defeito real. Com o prompt do UAC aberto, a
árvore de acessibilidade mostrava:

```text
ElevationProgress=<absent from tree>
ProgressText=<absent from tree>
InstallButton Enabled=True
CancelButton Enabled=True
```

O `SetVerificationInProgress(true)` em `MainPage.xaml.cs` era chamado antes do
pedido de elevação, mas o `Process.Start` em `ElevationVerifier.RequestAsync`
ocorre **antes de qualquer `await`**. Com `UseShellExecute` e `Verb = "runas"`,
`ShellExecuteEx` bloqueia a thread até o prompt ser respondido. A thread da UI
ficava travada, o XAML não executava o passe de renderização e o estado de
preparação nunca aparecia.

O defeito só passava despercebido porque a máquina de teste está com o UAC
reduzido ao mínimo: nesse modo a elevação é instantânea e a janela de
exibição é de milissegundos. No padrão do Windows, que é o que o usuário real
encontra, a tela ficaria morta e sem feedback durante todo o prompt.

A correção foi despachar o `Process.Start` para fora da thread da UI:

```csharp
elevatedProcess = await Task.Run(() => Process.Start(new ProcessStartInfo { ... }));
```

#### Revalidação sob UAC no padrão — 2026-09-29

Toda a validação foi refeita com `ConsentPromptBehaviorAdmin=5`, compilando
antes com zero erros e zero avisos. Os três caminhos foram exercitados de
novo, e desta vez com o prompt visível.

Estado observado **durante** o prompt, idêntico no caminho confirmado e no
cancelado:

```text
ElevationProgress Name='OcupadoVerificando permissão de administrador' Offscreen=False
ProgressText       Name='Aguardando a confirmação do Windows…' Offscreen=False
InstallButton      Enabled=False
CancelButton       Enabled=False
StatusInfoBar      <absent from tree>
```

O `ProgressBar`indeterminado aparece, os dois botões ficam desabilitados e o
`InfoBar` permanece fechado até haver resultado. A correção está validada.

Estado final em cada caminho:

| Caminho | InfoBar | Botão primário |
| --- | --- | --- |
| Confirmado (usuário aprovou) | `Ícone de Êxito` / "Permissão confirmada" | "Verificar novamente" |
| Cancelado (usuário recusou) | `Ícone de aviso` / "Nada foi alterado no computador." | "Instalar" |
| Falha (lançamento bloqueado) | `Ícone de erro` / "Não foi possível continuar" | "Instalar" |

O caminho de falha foi provoked de forma determinística e sem tocar em
política: o instalador já em execução teve o seu executável tomado com
`FileShare.None`. A imagem do processo já está mapeada, então o aplicativo
continua funcionando, mas qualquer segundo lançamento do mesmo arquivo falha.
Isso exerciseu o `catch` genérico de `RequestAsync` com uma falha real de
`Process.Start`, e não um cenário artificial. A mensagem exibida foi a do
sistema, sobre o arquivo em uso.

Em todos os três casos, ao final, o `ProgressBar` e o `ProgressText` saem da
árvore, os dois botões voltam a ficar habilitados e o `InfoBar` traz o
resultado correspondente.

#### Caminho já elevado — 2026-09-29

Um usuário pode acionar **Executar como administrador** no menu de contexto.
Nesse caso `RequestAsync` retorna em `IsProcessElevated()`, sem criar pipe e sem
lançar sonda. O instalador foi iniciado com `schtasks /IT /RL HIGHEST` para
cobrir esse caminho:

```text
RUN_IS_ADMIN=True
CONSENT_PROMPTED=False
Ícone de Êxito
A permissão de administrador já está ativa para este instalador.
INSTALL_LABEL_AFTER=Verificar novamente
ELEVATION_PROGRESS_PRESENT=False
```

Nenhum prompt é apresentado, o que é o comportamento correto: não há o que
consentir. O `ProgressBar` não aparece porque a resposta é imediata.

#### Sub-ramos defensivos ainda sem exercício

`RequestAsync` tem mais um caminho de falha que **não** foi exercitado: o
processo que não responde, o exit code inesperado, o pipe que não recebe
conexão, o timeout de 60s e a exceção genérica durante a espera. Só o `catch`
de `Process.Start` foi provocado.

O timeout é o mais relevante dos cinco, porque é o único que um usuário real
poderia encontrar. Ele **não** decorre de demora do usuário no prompt: o
`CancellationTokenSource` de 60s é criado depois que `Process.Start` retorna,
ou seja, só depois da resposta. Ele cobre apenas a falha de handshake depois
do consentimento.

Esses ramos não foram testados porque provocá-los exigiria sequestrar o pipe
nomeado em uma corrida de poucos milissegundos contra a sonda, o que produziria
um teste frágil e dependente de timing. Ficam registrados como cobertura
conhecida, a ser reavaliada com injeção de falha quando o processo elevado
passar a executar a instalação de verdade.

#### Limpeza

Ao final, `ConsentPromptBehaviorAdmin` foi restaurado para `0`, os processos
`CompassoInstaller` e `consent` foram encerrados e as tarefas agendadas de
teste foram removidas. Nenhum processo do Compasso ficou em execução.

## Observações de encerramento do item 4

O quarto item do checklist está **concluído**, com uma ressalva declarada.

O que está validado e serves de contrato:

- os quatro caminhos de elevação alcançáveis por usuário — **Confirmado**,
  **Cancelado**, **Falha** e **Já elevado** — exercitados de ponta a ponta na
  interface, sob `ConsentPromptBehaviorAdmin=5`, o padrão do Windows;
- os estados de preparação observados com o prompt aberto: barra indeterminada
  visível, texto "Aguardando a confirmação do Windows…" e os dois botões
  desabilitados;
- o retorno correto ao estado ocioso nos quatro casos;
- a correção do bloqueio da thread da UI, em `ElevationVerifier.cs`, aplicada e
  revalidada.

A ressalva: **cinco sub-ramos defensivos de falha permanecem sem exercício** —
processo que não responde, exit code inesperado, pipe sem conexão, timeout de
60s e exceção genérica durante a espera. Apenas o `catch` de `Process.Start`
foi provocado.

Eles não foram testados porque exigem sequestrar o pipe nomeado numa corrida de
poucos milissegundos contra a sonda, o que produziria um teste frágil e
dependente de timing. Teste que depende de corrida é pior do que cobertura
declarada.

A decisão do usuário em 2026-09-29 foi encerrar o item com essa ressalva
registrada, e tratar a cobertura junto do quinto item, onde o processo elevado
passará a executar a instalação de verdade e haverá o que injetar falha.

Observação adicional: a validação só tem valor sob UAC no padrão. Com
`ConsentPromptBehaviorAdmin=0` a elevação é instantânea e **esconde justamente
o defeito de interface que foi encontrado e corrigido**. Isso não é um detalhe
da máquina de teste; é o defeito que o usuário real teria visto.

## Decisão de arquitetura do instalador — 2026-09-29

Aprovada pelo usuário para o quinto item do checklist.

**A sonda elevada deixa de ser sonda e vira trabalhador de longa duração, com o
pipe nomeado em duplex.** Um único pedido de UAC, um único processo elevado, e
o instalador conduz o trabalho por cima do canal já validado.

O caminho descartado era verificar a elevação e depois relançar o executável
elevado para instalar. Ele foi rejeitado por exigir duas elevações, ou uma
cadeia frágil, e por deixar o segundo processo sem canal para reportar
progresso ao usuário.

O argumento decisivo a favor do duplex é que o canal já está validado na
direção exata de que a produção precisa: servidor não elevado, cliente
elevado, com `PipeOptions.CurrentUserOnly` funcionando. Estender o que já
funciona é menor risco do que introduzir um segundo mecanismo de elevação.

Custo aceito: um protocolo pequeno de mensagens em JSON sobre o pipe, com
comando, eventos de progresso e resultado final.

**Layout de destino, todos exigindo elevação e válidos para todas as contas:**

| Elemento | Caminho |
| --- | --- |
| Componentes instalados | `C:\Program Files\Compasso\` |
| Atalho no menu Iniciar comum | `C:\ProgramData\Microsoft\Windows\Start Menu\Programs\Compasso\` |
| Entrada de desinstalação | `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Compasso` |

**Sequenciamento.** O quinto item instala serviço e interfaces, e nenhum dos
dois existe: o serviço foi revertido e as interfaces ainda não foram escritas.
Decidido não instalar carga de mentira apenas para exercitar o motor. O motor
de instalação será construído primeiro e o primeiro payload real será a
interface **Adicionar tempo**, que é o item 6 puxado para dentro do item 5,
porque uma coisa sem a outra não é demonstrável. Isso respeita a ordem
registrada: instalador primeiro, interfaces depois.

## Motor de instalação duplex — 2026-09-29

O motor foi implementado e validado com trabalho real no disco. As telas do
produto continuam pendentes, e por isso nada foi instalado de verdade: o que
foi exercitado foi um plano de teste contido.

### Arquivos

| Arquivo | Papel |
| --- | --- |
| `InstallProtocol.cs` | Envelope único de mensagem, plano e contexto JSON gerado em tempo de compilação |
| `InstallEngine.cs` | Lado não elevado: pede elevação, abre o pipe, bombeia mensagens |
| `ElevatedWorker.cs` | Lado elevado: anuncia permissão e atende comandos até o encerramento |
| `InstallPlanExecutor.cs` | Aplica o plano: diretório, cópia, atalho, registro |
| `InstallPlanFactory.cs` | Lê `payload.json` do pacote e monta o plano |
| `ElevationVerifier.cs` | Reduzido a roteamento do argumento e à verificação de privilégio |

### Decisões de implementação

O `payload.json` fica no pacote e é a fronteira entre o que o motor sabe fazer e
o que ele transporta. O motor não conhece nenhuma interface, então serve para a
primeira e para as próximas sem alteração de código.

A serialização é gerada em tempo de compilação. O projeto publica com
`PublishTrimmed` ligado, e a serialização por reflexão seria removida pelo
trimmer em Release, quebrando o canal só na build distribuível.

Quando o instalador já está elevado, o trabalhador roda dentro do próprio
processo em vez de relançar um segundo. O caminho de execução continua sendo um
só, o que evita dois comportamentos para o mesmo código.

A cópia grava em arquivo temporário e move por cima do destino, para que uma
interrupção não deixe um executável truncado no lugar.

### Evidência

Plano de teste com 12 arquivos, diretório e atalho próprios, chave de registro
de teste e executado na sessão `1` sob `/RL HIGHEST`:

- `RX_TOTAL=32`, sendo `RX_PROGRESS=30` e um `result` final. Trinta eventos de
  progresso antes do resultado é a prova de que o canal é duplex de verdade e
  não apenas um pedido seguido de uma resposta.
- Arquivos copiados com conteúdo conferido, atalho criado com alvo e descrição
  corretos, entrada de registro com nome e versão corretos.
- Nenhum arquivo temporário `.compasso-staging` remanescente.

### Defeito encontrado e corrigido na validação

O primeiro passo do plano era rotulado com a constante
`InstallPaths.InstallDirectory` em vez de `plan.InstallDirectory`. O disco
recebia o caminho correto e a tela anunciava outro. Para o usuário seria uma
tela que mente sobre onde está instalando, então o rótulo passou a vir do
plano. A captura do canal é o que revelou isso: sem ela o defeito teria
passado, porque a instalação teria funcionado.

### O que não foi provado

A validação correu pelo caminho já elevado, o que não exige clique no UAC. O
caminho com prompt real, `Verb="runas"` e aceite do usuário, não foi
revalidado com o motor novo. O handshake de elevação em si já tinha sido
validado no item 4; o que falta é confirmar o prompt com a sessão duplex viva,
o que exige alguém clicando em **Sim**.

## Resíduo encontrado na máquina

`C:\ProgramData\Microsoft\Windows\Start Menu\Programs\Compasso` contém dois
atalhos de 22/09/2026 que apontam para
`C:\Program Files\Compasso\Agent\compasso-windows-bonus.exe` e
`compasso-windows-setup.exe`. Nenhum dos dois arquivos existe, e a pasta
`C:\Program Files\Compasso` está ausente. São resíduo de uma sessão anterior
que chegou a construir as interfaces antes de revertê-las.

Não foram removidos. São estado do sistema e a remoção precisa de autorização
explícita. Vale observar que o atalho antigo diz **Configurar o agente**, nome
que não bate com **Configurações** usado no resto do material.

## Interfaces Adicionar tempo e Configurações — 2026-09-29

O projeto `windows/CompassoApp` foi criado a partir do esqueleto do instalador,
sem os arquivos do motor de elevação. As duas telas navegam entre si: o rodapé
de Adicionar tempo abre Configurações, e o link de Configurações volta.

### Extração do conceito

Os PNG não são legíveis como imagem. O conteúdo e a geometria foram extraídos
com OCR e amostragem de pixels, e registrados em
`docs/design/windows-shell/README.md`. A ferramenta foi instalada num ambiente
virtual descartável e removida depois do uso.

A vetorização para SVG foi descartada antes de ser tentada: o potrace reproduz
o desenho pixel a pixel e devolveria dezenas de milhares de coordenadas sem
nenhum conteúdo legível. O que faltava era o texto e a posição dos controles,
e é isso que o OCR entrega.

Depois da extração, o usuário corrigiu três pontos que o OCR não distingue: o
aviso da senha tem ícone de informação e está em itálico, o acesso a
Configurações fica no rodapé com ícone de engrenagem, e as caixas de tempo são
selecionáveis. A descrição do usuário prevalece.

### Validação

Execução na sessão `1` com inspeção por UI Automation:

- Adicionar tempo: marca, título, subtítulo, chip de servidor, quatro caixas de
  tempo na mesma linha, campo de senha, botão de confirmação, aviso em itálico e
  rodapé com Configurações. Todos presentes e em ordem.
- Navegação Adicionar tempo para Configurações: `NAV_TO_SETTINGS=OK`.
- Configurações: chip de estado, conta Windows, as três seções de conexão com
  servidor, identificador e token, ação `Mostrar`, botão `Salvar e conectar` e
  retorno. Todos presentes e em ordem.
- Navegação de volta: `NAV_BACK=OK`.
- `RESULT=PASS`, build com zero erros e zero avisos.

### Defeitos encontrados na validação

Três falhas sequenciais, todas na abertura da tela, todas visíveis no
`startup.log`:

1. `IsChecked="True"` no `RadioButton` do XAML dispara `Checked` durante
   `InitializeComponent`, antes de o construtor conseguir ler `Content`, e o
   `ToggleButton.IsChecked` não pode ser atribuído nessa fase.
2. Duas raízes na `Page` depois que o rodapé foi movido para fora do
   `ScrollViewer`, o que viola a regra de conteúdo único.
3. `Application.Current.Resources["CompassoDurationSelectedBrush"]` devolvia um
   `Style`, não um `Brush`, porque o pincel estava declarado como `Style` com
   `Setter` em vez de `SolidColorBrush`.

As três foram corrigidas e o aplicativo passou a abrir. O `startup.log` é o que
permitiu ver isso: pela saída da interface, as três se pareciam com "o app não
abriu".

### Ajustes visuais pedidos pelo usuário

Removidos o rótulo da marca no corpo, que já aparece no cabeçalho da janela, e
o rótulo "Período", que ocupava uma linha sem função. Os blocos de tempo
escolhidos passaram a receber o coral da marca como fundo inteiro, com texto
branco, e não apenas a borda.

O hover azul no botão de confirmação foi corrigido. A causa é que
`AccentButtonStyle` anima o fundo por `ObjectAnimationUsingKeyFrames` lendo
recursos `AccentButton*` do sistema, e o `Button` do WinUI 3 não expõe
`BackgroundPressed` nem `BackgroundDisabled` como propriedades definíveis em
estilo: o compilador XAML rejeita os `Setter`. A correção foi reescrever o
`ControlTemplate` do botão e apontar cada estado para os pincis da marca.

O `AutomationProperties.Name` do botão de confirmação foi removido. Ele fixava
o nome acessível em "Confirmar adição de tempo" e mascarava o texto real, que
muda com a escolha do tempo. Com ele, a inspeção da interface lia sempre a
mesma frase e a validação de que o texto acompanha a escolha não teria como
acertar.

### Validação dos ajustes

`RESULT=PASS` com build em zero erros e zero avisos:

- `MARCA_NO_CORPO=False` e `ROTULO_PERIODO=False`: os dois rótulos sumiram.
- As quatro caixas de tempo e o botão de confirmação presentes e invocáveis.
- `BOTAO_ADD_ROTULO=Adicionar 30 minutos` e, após clicar em 60,
  `APOS_CLIQUE_60=Adicionar 60 minutos`: o texto acompanha a escolha.

### Dimensoes da janela

A janela media 1000x1080, muito maior que o conteudo. A causa era o
`HorizontalAlignment="Center"` no `StackPanel` do corpo: com Center o painel
encolhe ate o conteudo, entao o `MaxWidth="780"` nunca era alcancado e a
coluna ficava com 492 px, deixando as laterais da janela vazias e a barra de
rolagem aparecendo sem necessidade.

Trocado por `HorizontalAlignment="Stretch"`, que respeita o `MaxWidth`. Com a
coluna em 780 px e a margem de 48 px, a janela passou a 876 px de largura. A
altura ficou em 780 px, que acomoda o conteudo de Adicionar tempo com 8 px de
folga em cada borda. Medido por UI Automation: os quatro botoes de tempo
ocupam 182 px cada e o campo de senha usa a largura cheia da coluna.

O mesmo `Center` estava em `SettingsPage`, corrigido junto. A marca duplicada no
corpo dessa tela tambem foi removida, ja que a janela mostra a marca no
cabecalho.

### Blocos de tempo preenchidos e janela reduzida

O texto dos blocos de tempo passou a ser branco. Branco so teria leitura sobre
o coral, o que confirmou que os quatro blocos deviam ser preenchidos com a cor
do botao de confirmacao, e nao apenas o escolhido. `CompassoDurationButtonStyle`
agora deriva de `CompassoPrimaryButtonStyle`, entao herda o coral, o texto
branco e o template com hover e pressed da marca. O codigo so alterna um anel
branco de 3 px no bloco escolhido. O recurso `CompassoDurationSelectedBrush`,
usado antes so para a borda do bloco escolhido, foi removido por ficar sem uso.

A janela foi reduzida em 50% na horizontal e 20% na vertical, de 876x780 para
438x624. A margem lateral do conteudo caiu de 48 px para 24 px e o espacamento
entre blocos de 12 px para 8 px, para que os quatro caibam na nova largura. O
`ScrollViewer` passa a absorver a altura que sobra, ja que o conteudo e mais
alto que a janela reduzida.

### Tela de Configuracoes reescrita

A tela segui a estrutura pedida: "Voltar para adicionar tempo" no inicio, o
icone maior da marca centralizado, "Configuracoes do Compasso" logo abaixo e
"Conecte o computador ao seu painel familiar" menor que esse. A marca nao se
repete no corpo, porque ja aparece no cabecalho da janela.

"Conta Windows" virou um seletor que lista as contas locais de verdade, por
`NetUserEnum` do netapi32, com queda para a conta atual se a API nao responder.
O endereco do servidor vem preenchido. O token permanece em `PasswordRevealMode`
oculto, com o botao "Mostrar" ao lado e o aviso "O token fica protegido neste
computador" abaixo do campo. "Salvar e conectar" usa o mesmo estilo do botao de
confirmacao da tela de tempo. A confirmacao do bloqueio de sessao virou
`CheckBox` e passou a ser exigida antes de tentar salvar.

### Configuracoes 30% mais larga e Voltar no fim

Configuracoes passou a abrir em 569x624, enquanto Adicionar tempo segue em
438x624: exatamente 30% a mais na horizontal, com a altura igual. A janela e
redimensionada a cada navegacao, em `RootFrame_Navigated`, que escolhe 569 para
`SettingsPage` e 438 para as demais telas. A coluna de conteudo de
Configuracoes subiu para 520 px, para acompanhar a janela mais larga.

O aviso "Ainda nao configurado" foi centralizado, junto com o icone e o titulo.

"Voltar para adicionar tempo" saiu do inicio e foi para o fim da tela, abaixo
de "Salvar e conectar", com a seta de volta e centralizado. Como nao ha mais
nada depois dele, ele fecha a hierarquia visual da tela.

Um detalhe da medicao: a primeira verificacao acusou um deslocamento de 66 px do
centro, mas era erro do proprio script, que media o retangulo da janela antes de
navegar e comparava a coluna de 569 px contra a janela de 438 px ainda aberta.
Relendo o retangulo depois da navegacao, todos os elementos de cabecalho e
campo ficam com `relCentro=0`.

### Comportamento honesto das telas

Nenhuma das telas simula sucesso. Adicionar tempo responde que o serviço ainda
não está instalado, e Configurações responde o mesmo ao salvar, porque o
serviço de fato não existe. Um "adicionado com sucesso" seria mentira sobre o
estado do computador.

O título da janela foi corrigido de "Compasso – Instalador" para "Compasso": o
aplicativo do computador não é o instalador, e a barra de título estava
divulgando a peça errada.

## Próximo passo histórico da tentativa WinUI — substituído

O texto abaixo foi preservado como contexto da tentativa anterior. O próximo
passo vigente está no início deste documento.

Implementar a instalação de verdade, que é o quinto item do checklist: o
processo elevado passa a executar o trabalho em vez de apenas confirmá-lo.
Antes de escrever código, decidir e registrar no log **como** o processo
elevado recebe o trabalho a fazer, porque a escolha é entre carregar comandos
pelo pipe nomeado já existente e fazer o processo elevado assumir a instalação
inteira. A decisão precisa incluir destino dos arquivos, criação do ícone e
registro da desinstalação.

## Problemas conhecidos

- A execução direta iniciada por SSH ocorre na sessão não interativa `0` e
  falha em `Microsoft.UI.Input.dll`; a validação de UI precisa ser iniciada na
  sessão interativa do usuário, como foi feito neste teste.
- A captura tradicional de tela não registra o conteúdo composto por GPU do
  WinUI nessa sessão remota. A existência e o conteúdo da janela foram
  verificados por HWND e UI Automation; a avaliação visual humana ainda é
  necessária.
- A sessão SSH já é elevada e por isso não serve para validar elevação. Use
  `schtasks /IT /RL LIMITED` para obter um processo não elevado na sessão `1`.
- O `InfoBar` de falha exibe a mensagem crua do sistema, incluindo o caminho
  completo do executável. Antes do lançamento real isso deve ser revisto para
  não vazar estrutura de diretórios na interface.
- O `startup.log` em build de depuração acumula mensagens de inicialização e
  probe. Ele fica ao lado do executável e não é removido entre execuções.

## Alinhamento de Adicionar tempo e Configurações — 2026-09-29

> **Correção registrada em 2026-09-29:** esta seção anterior chamou o
> alinhamento de concluído com base em texto e UI Automation, sem comparar as
> imagens renderizadas. Essa conclusão foi incorreta e está retirada. A
> reavaliação abaixo é a fonte de estado atual; o item visual do checklist
> permanece aberto até haver captura do app e comparação direta com os PNG.

Revisado o layout das duas telas em `docs/design/windows-shell/` contra a
implementação vigente, `windows/CompassoApp` (WinUI 3). O instalador
`CompassoInstaller` continua sendo a etapa responsável por instalar os
componentes; este trabalho não copia telas para o instalador nem tenta
implantar o serviço.

Na tela **Adicionar tempo**, o indicador deixou de afirmar que o servidor está
conectado: mostra **“Serviço não instalado”**, que é o estado real neste ponto
do projeto. Removi o nome de automação que escondia esse texto visível. Mantive
as quatro durações, a confirmação e a navegação para Configurações nos termos
do layout existente.

Em **Configurações**, incluí o cabeçalho **“Conta que será controlada”**,
ajustei o subtítulo para o texto definido no conceito (“Conecte este computador
ao seu painel familiar.”) e retirei o tratamento de cartão arredondado da
confirmação de consentimento, para que volte a ser um checkbox simples como no
desenho. Também retirei nomes de automação fixos que mascaravam o texto real
do título, do estado e da confirmação; os nomes visíveis passam a ser lidos
corretamente por tecnologia assistiva/UI Automation. Os nomes explícitos dos
campos continuam identificando servidor, dispositivo e token.

### Verificação

- Build Debug x64 em `C:\CompassoWinUIBootstrap\unpackaged\CompassoApp`:
  zero erros e zero avisos.
- UI Automation na janela interativa encontrou o título, subtítulo, estado,
  cabeçalho da conta, seletor Windows, consentimento, campos de conexão,
  revelar token, salvar e voltar em Configurações.
- A navegação para Configurações e a validação do respectivo conteúdo foram
  exercitadas. A checagem automatizada de retorno ainda está em execução; por
  isso não registro aqui uma nova evidência de navegação bidirecional.
- Não foi feita inspeção visual humana por screenshot nesta sessão remota
  (captura GPU indisponível). A comparação foi de conteúdo/layout estrutural,
  não uma aprovação visual pixel a pixel.

### Estado

Checklist separa agora a implementação/alinhamento das duas interfaces, já
concluídos, da integração com o serviço real, ainda pendente. O próximo marco
continua sendo a instalação do serviço pelo instalador; só depois as ações de
adicionar tempo e salvar configuração poderão ser validadas funcionalmente.

## Revisão visual direta dos conceitos — 2026-09-29

Etapa 1 — comparação dos arquivos de referência com a implementação vigente
`windows/CompassoApp`. As imagens foram abertas em resolução original lado a
lado com os XAML; não foi gerado um conceito alternativo, pois os PNG existentes
são a especificação que o usuário pediu para implementar. Skills aplicadas:
`frontend-app-builder` para o fluxo conceito→fidelidade e `winui-app` para a
implementação nativa WinUI. `imagegen` não é necessário para esta correção: o
produto exige controles e marca vetorial code-native, e há conceito aprovado.

Diferenças confirmadas antes da implementação:

| Superfície | Conceito | Implementação encontrada | Ação planejada |
| --- | --- | --- | --- |
| Janela Adicionar tempo | proporção aproximada 504×634 DIP | 438×624 | aproximar a dimensão útil ao conceito e manter as quatro opções na mesma linha |
| Tipografia de títulos | serifada de display | Segoe UI sem serifa | usar Georgia local, sem dependência de fonte remota |
| Marca de Configurações | símbolo Compasso + nome centralizados | imagem placeholder do template (“X”) e sem nome | substituir pelo símbolo vetorial da marca e wordmark |
| Ícone da barra de título | símbolo Compasso | ícone placeholder do template | gerar o `.ico` a partir do mesmo símbolo |
| Estado em Adicionar tempo | ponto verde e “Servidor conectado” | ponto verde com “Serviço não instalado” | manter o texto verdadeiro e trocar a semântica visual do ponto para estado indisponível |
| Campo de senha principal | superfície alta e ação de revelar | altura reduzida; revelar não confirmado no controle atual | dimensionar o campo e validar o revelar nativo |
| Conta e checkbox | coluna larga, consentimento explicitamente assinalado no mock | consentimento não marcado inicialmente | manter desmarcado para exigir consentimento deliberado; diferença funcional intencional |

Tokens visuais de referência mantidos: fundo `#F3F4F1`/`#F5F5F3`, texto
`#261D2B`, coral `#DD6D50`, sage `#819484`, chip `#D9DCD8` e linha
`#D8D5D7`. A imagem do token preenchido no conceito não será copiada como valor
de exemplo: nenhum segredo fictício será exibido.

Resultado desta etapa: o checklist foi corrigido para separar “telas
implementadas” de “fidelidade visual pendente”. Nenhuma alteração visual foi
declarada concluída nesta etapa. Próxima etapa: ajustar os XAML e recursos
compartilhados do `CompassoApp`, depois compilar e capturar as duas telas para
comparação direta.

## Primeira captura e comparação renderizada — 2026-09-29

Etapa 2 — build e execução da primeira rodada de ajustes.

- Build Windows Debug x64: sucesso, zero avisos e zero erros.
- Capturas reais da janela: Adicionar tempo `504×634`, Configurações `530×688`.
- UI Automation confirmou o conteúdo esperado nas duas telas e a navegação de
  ida e volta.
- A captura de tela via `CopyFromScreen` conseguiu registrar o conteúdo WinUI
  renderizado nesta execução, então a comparação visual direta foi possível.

Diferenças ainda visíveis nas capturas desta rodada (não concluídas): o
subtítulo de Adicionar tempo quebra em duas linhas embora o conceito o mostre
em uma; Configurações fica cortada no identificador do dispositivo, sem exibir
token, ação primária e retorno de uma vez; a barra de título ainda mostra o X
do asset de template porque o `.ico` foi copiado para a raiz do espelho, não
para `Assets`; e a conta selecionada aparece como `windows11\\Sergio` em vez
do nome curto do mock (`Sergio`). A etapa seguinte corrige essas divergências
e repete build, screenshot e UI Automation.

## Ajustes de fidelidade e comparação final — 2026-09-29

Etapa 3 — rodada final de ajustes em `windows/CompassoApp` e comparação das
capturas renderizadas com os conceitos originais.

### Implementação

- Substituído o ícone X do template pela marca de barras do Compasso no
  cabeçalho da janela; Configurações agora usa o lockup da marca centralizado
  com o nome, não o PNG placeholder do template.
- Títulos passaram à família serifada local `Georgia`, e as dimensões das
  janelas acompanham a proporção dos conceitos: Adicionar tempo `504×634`,
  Configurações `530×708`.
- Adicionar tempo agora mantém o subtítulo em uma linha, apresenta o status
  factual sem chip verde de sucesso, mantém as quatro durações selecionáveis,
  exibe olho funcional no campo de senha e fixa Configurações no rodapé.
- Configurações recebeu controles compactos para caber na janela da referência;
  a sequência de campos, ação primária e link de retorno aparece junta. A
  seleção Windows mostra o nome curto `Sergio`.
- Revelar/ocultar senha e token foi verificado, e os nomes acessíveis acompanham
  o estado de cada botão. O token continua oculto por padrão.

### Comparação visual documentada

| Ponto conferido | Resultado no render final |
| --- | --- |
| Barra do Windows e marca | Ícone de barras e título Compasso visíveis; X placeholder removido |
| Hierarquia dos títulos | Títulos serifados e alinhamento central de Configurações correspondem ao conceito |
| Fundo, chip e campos | Fundo claro, chip neutro, contornos e superfícies preservam a paleta amostrada |
| Adicionar tempo | Subtítulo em uma linha, quatro opções em uma linha, senha com olho, ação coral e rodapé |
| Configurações | Lockup, subtítulo, status, conta, consentimento, três campos, botão salvar e retorno aparecem na tela |
| Navegação e controles | UI Automation confirmou ida e volta; revelar senha/token alternou os nomes acessíveis |

Capturas finais do app, feitas a partir da janela interativa do Windows e
inspecionadas com `view_image` junto dos conceitos originais:

- `docs/design/windows-shell/validation/add-time-final.png` — `504×634`.
- `docs/design/windows-shell/validation/settings-final.png` — `530×708`.

Build Debug x64 executado no Windows após as mudanças funcionais: sucesso, zero
erros e zero avisos. O teste interativo terminou com `LastTaskResult=0`; a
árvore UIA confirmou textos, dimensões, navegação e estados dos botões de
revelar. `git diff --check` também passou.

### Exceções visuais intencionais

- “Serviço não instalado” e o ponto neutro substituem “Servidor conectado” e o
  ponto verde do PNG: o serviço ainda não está instalado.
- O consentimento abre desmarcado, embora o mock mostre a caixa marcada, para
  exigir uma ação deliberada antes de aceitar o bloqueio da sessão.
- O campo de token começa vazio, em vez de repetir os pontos de uma credencial
  ilustrativa presente no mock.
- O aviso da senha usa “A senha não será armazenada.”, conforme o texto já
  confirmado para esta interface; o conceito gráfico mostra “A senha não é
  armazenada.”.
- Os quatro blocos de tempo ficam preenchidos de coral por decisão anterior
  explícita do usuário; o bloco escolhido recebe contorno branco.
- A seta no retorno de Configurações é mantida conforme instrução anterior do
  usuário, embora o PNG mostre apenas o texto.

O item visual do checklist pode ser fechado com essas exceções registradas.
Isso não conclui integração: adicionar tempo e salvar configuração continuam
sem falar com um serviço real.

## Correção do pacote entregue pelo instalador — 2026-09-29

Na primeira entrega, as duas telas tinham sido alteradas e validadas ao abrir
`CompassoApp.exe` diretamente da pasta de build, mas isso não atualizava o
executável já instalado nem o payload ao lado do instalador que o usuário
estava abrindo. A checagem do Windows confirmou a causa: o app em
`C:\Program Files\Compasso\CompassoApp.exe` era anterior, e duas fontes de
Configurações no espelho de build também não correspondiam ao repositório.

Corrigida a cadeia completa do artefato de teste, sem alterar implementações
legadas:

- Sincronizado todo o projeto vigente `windows/CompassoApp` para o espelho
  Windows, incluindo `AddTimePage` e `SettingsPage` e seus recursos.
- Build Windows do `CompassoApp`: sucesso, zero avisos e zero erros.
- Executado `build-payload.ps1`: pacote regenerado ao lado do instalador com
  534 arquivos, incluindo o app e as duas telas.
- Build Windows do `CompassoInstaller`: sucesso, zero avisos e zero erros.
- UI Automation repetida no build sincronizado: Adicionar tempo e Configurações
  encontradas; conteúdos, tamanhos, revelar/ocultar senha/token e navegação de
  ida e volta passaram (`LastTaskResult=0`). As capturas nesta pasta foram
  substituídas pelas dessa execução.

O artefato atualizado está em
`C:\CompassoWinUIBootstrap\unpackaged\CompassoInstaller\bin\x64\Debug\net10.0-windows10.0.26100.0\win-x64\CompassoInstaller.exe`, com `payload.json` e `payload/app` na mesma pasta. **A instalação/atualização em `C:\Program Files` não foi executada nesta etapa**, portanto a cópia já instalada ainda é antiga; requer executar o instalador atualizado e aceitar UAC. Isso é separado do build do pacote e não é apresentado como concluído.

## Diagnóstico: fechar e instalar novamente — 2026-09-29

Inspecionados os handlers ativos e o `startup.log` do instalador Windows após
uma instalação e uma desinstalação reais. Nenhum código foi alterado nesta
etapa.

- **Instalar após desinstalar não faz nada:** `UninstallButton_Click` mantém a
  sessão elevada em `_session` depois de concluir e exibe novamente Instalar.
  Porém `InstallButton_Click` retorna imediatamente quando `_session` não é
  nula. `EnsureElevatedAsync` já sabe reutilizar uma sessão; a condição de
  retorno antecipado impede que essa lógica seja alcançada. O handler de
  desinstalação tem o mesmo retorno antecipado, impedindo também operações
  subsequentes quando já existe uma sessão.
- **Fechar demora:** enquanto uma operação está ativa, `SetBusy(true)` desativa
  o botão Fechar. Depois, o handler de Fechar espera `ShutdownAsync` e
  `DisposeAsync`, que aguardam o trabalhador elevado sair (até 5 segundos).
  O log registra que, após os resultados de instalação (`19:08:00`) e
  desinstalação (`19:08:43`), o worker termina com
  `ObjectDisposedException: Cannot access a closed pipe` em
  `ElevatedWorker.RunAsync`, linha 98. `StreamReader` e `StreamWriter` foram
  criados sobre o mesmo pipe com ownership padrão: ao sair, o reader fecha o
  pipe antes de o writer fazer flush/dispose. Isso explica o encerramento
  anormal e a espera do botão Fechar; não é o botão de fechar da barra de título,
  que ainda não tem handler próprio.

A correção deve separar “há uma sessão elevada reutilizável” de “há uma
operação em andamento”, e fazer reader/writer deixarem de fechar o mesmo pipe
um do outro (ou coordenar explicitamente o descarte). Pendentes: implementar e
revalidar instalar → desinstalar → reinstalar, além do encerramento normal e do
fechamento durante operação.

## Ícone da aplicação com a raposa dos computadores — 2026-09-29

O usuário esclareceu que o ícone do `CompassoApp` deve usar a raposa que já é
usada como avatar de máquina, não a marca de barras do Compasso. Fonte
confirmada: `admin-ui/src/assets/illustrations/avatars/fox.webp`.

- Exportada a mesma imagem para `windows/CompassoApp/Assets/FoxAvatar.png`.
- A barra de título agora usa `FoxAvatar.png`; o `AppIcon.ico` multirresolução
  foi regenerado com a mesma raposa para a janela, o executável, a barra de
  tarefas e o atalho criado a partir dele. A marca de barras continua no
  cabeçalho próprio da tela Configurações.
- Build Windows Debug x64 do app e do instalador: sucesso, zero avisos e zero
  erros. Payload recomposto com 535 arquivos.
- UI Automation nas duas telas e navegação de ida e volta: aprovado
  (`LastTaskResult=0`). A captura `add-time-final.png` mostra a raposa na barra
  de título; o app permaneceu aberto após validação.

A cópia instalada só recebe o novo ícone após atualizar a instalação pelo
instalador recompilado; esta etapa atualizou o executável e o payload, não
executou UAC no sistema.

## Diagnóstico e correção do ícone no título, taskbar e busca — 2026-09-29

O ícone estava embutido no executável e a captura mostrava a raposa no ícone
XAML do título, mas isso não comprovava o ícone Win32 da janela nem a barra de
tarefas. A causa no app era chamar `AppWindow.SetIcon` com o caminho relativo
`Assets/AppIcon.ico`; o contrato do Windows App SDK pede caminho completo.

- `MainWindow` agora resolve o ícone via `Path.Combine(AppContext.BaseDirectory,
  "Assets", "AppIcon.ico")` e chama também `SetTaskbarIcon` explicitamente.
- O script de payload agora cria o atalho apontando para
  `C:\Program Files\Compasso\Assets\AppIcon.ico`, não para a extração do
  executável.
- Reconstruído o `CompassoApp.exe` Debug x64 e feito `Rebuild` do
  `CompassoInstaller.exe`; ambos terminaram sem erros ou avisos. Payload
  regenerado com 535 arquivos e o manifesto aponta o atalho para o `.ico` da
  raposa.
- UI Automation iniciou o app atualizado e confirmou as duas telas, os campos,
  os controles e a navegação (`LastTaskResult=0`). A captura mostra a raposa no
  título; processo de teste ficou aberto em
  `C:\CompassoWinUIBootstrap\unpackaged\CompassoApp\...\CompassoApp.exe`.
- Confirmei que a imagem extraída do executável instalado era a raposa, mas a
  cópia em `C:\Program Files\Compasso` tem timestamp anterior ao build final
  (`19:13` contra `19:32`). O atalho também só receberá o `IconLocation` direto
  do `.ico` após atualizar a instalação.

Portanto, a correção está no build verificado e no pacote novo, mas ainda não
está ativa na cópia instalada. Para concluir essa última propagação, é preciso
executar o instalador atualizado com elevação administrativa.

## Ícone do aplicativo no Shell do Windows — 2026-09-29

O usuário informou que desinstalou e instalou novamente, mas a busca do Windows
e a barra de tarefas continuaram sem o ícone. A instalação que ele repetiu era
do pacote anterior: nela o executável já tinha o recurso ICO e a janela já
retornava um HICON da raposa, mas faltava identidade explícita comum entre o
processo e o atalho. Portanto, a alteração anterior do ícone Win32 não bastava
para provar a associação que o Shell usa para busca e taskbar.

Também foi corrigida uma alteração indevida: o cabeçalho do `CompassoApp`
voltou a usar `CompassoBrandMark.png`. A raposa fica no ícone do executável,
janela, atalho e identidade do aplicativo; não substitui a marca no cabeçalho.

- Definido `Compasso.Multifamily.Desktop` no início do processo via
  `SetCurrentProcessExplicitAppUserModelID`.
- O atalho `.lnk` criado pelo instalador recebe o mesmo
  `System.AppUserModel.ID`; a entrada de desinstalação passa a registrar
  `DisplayIcon` para `Assets\AppIcon.ico` e `InstallLocation`.
- A orientação foi conferida na documentação oficial: o AppUserModelID
  explícito associa processo, janela e atalho; o atalho deve usar o mesmo ID
  definido pelo processo.
- Build Windows Debug x64 de `CompassoApp` e `CompassoInstaller`: sucesso, zero
  avisos e zero erros. Payload regenerado com 535 arquivos.
- UI Automation do executável recém-compilado: Adicionar tempo, Configurações,
  dimensões e navegação de ida e volta passaram. A captura confirma a marca
  original no cabeçalho; o HICON obtido da janela é a raposa.

O pacote corrigido está em
`C:\CompassoWinUIBootstrap\unpackaged\CompassoInstaller\bin\x64\Debug\net10.0-windows10.0.26100.0\win-x64\CompassoInstaller.exe`.
Não executei a instalação por UAC nesta sessão; portanto, a cópia em
`C:\Program Files\Compasso` ainda não recebeu este build. A confirmação final
de busca/taskbar depende de aplicar este pacote e observar a associação no
Shell; a validação de janela/HICON isoladamente não é apresentada como prova
de que a busca já esteja corrigida.

## Consolidação das correções e do processo de build — 2026-09-29

Esta seção substitui como estado atual as pendências e conclusões provisórias
das seções anteriores. A implementação vigente continua sendo apenas
`windows/CompassoApp` e `windows/CompassoInstaller`.

### Interfaces comparadas com os conceitos

- Corrigida a interpretação errada dos períodos da tela Adicionar tempo. Os
  botões em repouso agora têm fundo claro, borda sutil e texto escuro; somente
  o período selecionado tem fundo coral e texto branco. A seleção inicial de
  30 minutos e o texto do botão principal permanecem sincronizados.
- Configurações mantém a estrutura do conceito, e o token passou a ser um
  campo único com divisor e ação `Mostrar` integrada no lado direito.
- A marca de barras continua no cabeçalho das duas telas. A raposa é somente o
  ícone da aplicação no executável, janela, busca, atalho e taskbar. A alteração
  anterior que colocou a raposa no cabeçalho foi revertida.
- As capturas da instalação real foram atualizadas em
  `docs/design/windows-shell/validation/add-time-final.png` e
  `settings-final.png`. UI Automation confirmou conteúdo, dimensões,
  revelar/ocultar senha e token, navegação de ida e volta e janela responsiva
  (`LastTaskResult=0`).

### Ícone do Windows: causa e prova final

O AppUserModelID explícito estava correto, mas a busca ainda mostrava uma folha
em branco. A captura da busca revelou a causa remanescente: o manifesto passava
`AppIcon-<hash>.ico,0` para `IShellLink.SetIconLocation`, que já recebe o índice
em outro argumento. O atalho resultante continha `AppIcon-<hash>.ico,0,0`, um
caminho inválido para o Shell.

- O manifesto agora passa somente o caminho do `.ico`; o atalho instalado
  resulta em `AppIcon-b202e268cd9d.ico,0`.
- O nome do arquivo de ícone inclui os 12 primeiros caracteres do SHA-256,
  impedindo que uma troca futura reutilize uma entrada antiga do cache.
- O atalho recebe `System.AppUserModel.RelaunchCommand`,
  `RelaunchIconResource` e, por último, o mesmo `System.AppUserModel.ID` do
  processo. O instalador notifica o Shell após gravar tudo.
- `Get-StartApps` retorna `Compasso.Multifamily.Desktop`; `DisplayIcon` e
  `InstallLocation` estão presentes no registro.
- A captura reproduzível da busca mostrou a raposa tanto no resultado pequeno
  quanto no painel grande. Com o app aberto, a mesma captura mostrou a raposa
  no botão da barra de tarefas; o HICON da janela também é a raposa.

### Atualização e ciclo do instalador

- Quando o produto já está instalado, o botão principal agora mostra
  `Atualizar ou reparar`; não é mais necessário desinstalar antes.
- O instalador prepara todos os arquivos em `C:\Program Files\Compasso.new`,
  move a instalação anterior para `.previous`, ativa a nova árvore e restaura
  a anterior se a troca falhar. Arquivos removidos do payload não ficam
  misturados com a versão nova.
- A atualização tenta fechar `CompassoApp` normalmente e encerra somente esse
  processo se ele estiver em outra sessão e não responder em cinco segundos.
- Os bloqueios que impediam instalar novamente quando `_session` já existia
  foram substituídos por estado separado de operação em andamento.
- Os leitores e escritores do pipe agora usam `leaveOpen`, eliminando o
  `ObjectDisposedException` por descarte duplo. O botão Fechar e o fechamento
  da janela encerram a sessão elevada.
- Teste elevado reproduzível em uma única janela:
  `UNINSTALL=PASS`, `REINSTALL_SAME_SESSION=PASS`, `CLOSE=PASS`,
  `CLOSE_MS=529`. A instalação foi restaurada ao final.

### Build sem payload obsoleto

`windows/build-windows.ps1` é o ponto de entrada canônico. Ele compila o
instalador, que referencia o app, recompõe o payload e compara SHA-256 entre
cada arquivo da árvore recém-compilada e sua cópia empacotada. Com `-Install`,
aplica a atualização por cima da instalação existente em um PowerShell elevado.

Além disso, os projetos estão encadeados nos dois sentidos sem recursão:

- build de `CompassoInstaller` compila `CompassoApp` e refaz o payload;
- build direta de `CompassoApp` dispara a build do instalador e refaz o payload;
- falha na geração do payload faz a build inteira falhar.

Comando validado no Windows:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File `
  C:\CompassoWinUIBootstrap\unpackaged\build-windows.ps1 `
  -Configuration Debug -Platform x64 -Install
```

Resultado final dessa etapa: zero avisos, zero erros e 536 arquivos no payload.
A comparação inicial conferiu somente `CompassoApp.exe`; a seção posterior de
estados interativos registra a ampliação necessária para os 535 arquivos do
app, incluindo `CompassoApp.dll`, onde ficam as alterações de interface.

## Atalho opcional na área de trabalho — 2026-09-29

Adicionada ao instalador vigente a opção `Criar atalho na área de trabalho`.
Como a instalação é para todas as contas, o destino é o Desktop público:
`C:\Users\Public\Desktop\Compasso.lnk`.

- A caixa inicia marcada quando esse atalho já existe e desmarcada quando não
  existe, preservando a escolha nas atualizações interativas.
- O atalho usa o mesmo alvo, diretório de trabalho, descrição, ícone versionado
  e AppUserModelID do atalho do menu Iniciar.
- Uma atualização com a opção desmarcada remove um atalho anteriormente criado;
  marcar novamente o recria. A desinstalação remove o atalho em qualquer caso.
- O modo automatizado `--install-unattended` preserva o estado atual do atalho,
  sem mudar silenciosamente a escolha feita na interface.
- A opção fica desabilitada enquanto uma operação está em andamento, junto com
  os demais comandos do instalador.

Build encadeada Debug x64 concluída com zero avisos e zero erros; payload
regenerado com 536 arquivos. O teste elevado na mesma janela registrou:

```text
UNINSTALL=PASS
DESKTOP_SHORTCUT=PASS
REINSTALL_SAME_SESSION=PASS
DESKTOP_SHORTCUT_DISABLE=PASS
DESKTOP_SHORTCUT_REENABLE=PASS
CLOSE_MS=111
CLOSE=PASS
```

Após o teste, o atalho público existe e aponta para
`C:\Program Files\Compasso\CompassoApp.exe`, com `IconLocation` em
`AppIcon-b202e268cd9d.ico,0`. A instalação foi restaurada, UI Automation das
duas telas passou (`LastTaskResult=0`) e o aplicativo instalado permaneceu
aberto.

## Estado selecionado durante hover — 2026-09-29

O usuário identificou que o período selecionado perdia o coral durante hover e
ficava branco. A captura em repouso não exercitava esse estado e, portanto, não
era evidência suficiente. A causa era o `VisualState` interno do `Button`
nativo sobrescrever `Background` e `Foreground` atribuídos pelo code-behind.

- Os quatro períodos agora são `ToggleButton`, o controle nativo apropriado
  para uma escolha persistente.
- Um template específico separa `CheckStates` de `CommonStates`: não
  selecionados ficam claros com texto escuro; o selecionado fica coral com
  texto branco. `PointerOver` não altera essas cores; `Pressed` só reduz
  brevemente a opacidade.
- O code-behind controla apenas `IsChecked`, garantindo exatamente uma escolha
  ativa e mantendo o texto do botão principal sincronizado.
- `verify-add-time-hover.ps1` abriu a instalação real, confirmou por UI
  Automation `Duration30` com `ToggleState=On`, posicionou o cursor no centro do
  controle, esperou o hover e capturou a janela.
- A captura `docs/design/windows-shell/validation/add-time-selected-hover.png`
  mostra `30 min` coral com texto branco sob o ponteiro e os demais claros com
  texto escuro, conforme `add-time-concept.png`.
- Regressão completa posterior: Adicionar tempo, Configurações e navegação de
  ida e volta passaram (`LastTaskResult=0`). A aplicação instalada ficou aberta.

O verificador de pacote também foi corrigido nesta etapa. Comparar somente
`CompassoApp.exe` era insuficiente porque esse apphost pode manter o mesmo hash
quando `CompassoApp.dll` muda. `build-windows.ps1` agora compara SHA-256 dos 535
arquivos da saída contra os caminhos correspondentes do payload e falha ao
primeiro arquivo ausente ou divergente. A execução final registrou
`Payload verificado: 535 arquivos idênticos à build`.

## Troca exclusiva entre períodos — 2026-09-29

A validação anterior cobria o estado inicial e o hover, mas não exercitava a
troca de uma opção para outra. A implementação foi ajustada para expressar a
semântica correta no próprio controle: os quatro períodos agora são
`RadioButton` com o mesmo `GroupName`, estilizados como os segmentos do
conceito. Esta seção substitui a escolha de `ToggleButton` registrada acima.

- Clicar em outro período seleciona a nova opção e desmarca automaticamente a
  anterior; não é possível deixar duas opções ativas nem desmarcar a única
  ativa clicando nela novamente.
- O template visual foi preservado: repouso claro com texto escuro; somente a
  opção selecionada usa coral e texto branco; hover não apaga a seleção.
- `verify-add-time-selection.ps1` abre o executável instalado, realiza cliques
  físicos no centro dos controles e consulta a árvore de UI Automation depois
  de cada clique.
- Fluxo executado na instalação real: `30 → 60 → 15`. Resultados:
  `EXCLUSIVE_SELECTION=PASS`, `30=PASS`, `60=PASS`, `15=PASS`; em cada etapa
  havia exatamente uma seleção e o texto da ação principal correspondia ao
  período escolhido.
- A captura final
  `docs/design/windows-shell/validation/add-time-selection-15.png` mostra 15
  minutos coral com texto branco e 30/60/120 restaurados ao estado claro.
- O verificador de hover foi adaptado ao padrão de acessibilidade do
  `RadioButton` e executado novamente sobre a instalação: período inicial 30
  selecionado e `POINTER_OVER_SELECTED=PASS` (`LastTaskResult=0`).
- Build e atualização pelo caminho canônico concluídas com zero avisos e zero
  erros. O payload foi regenerado com 536 arquivos e todos os 535 arquivos do
  aplicativo foram conferidos contra a build por SHA-256. O SHA-256 de
  `CompassoApp.dll` no payload e em `C:\Program Files\Compasso` coincidiu
  (`4AB6BC58…DA4B39A`), comprovando que a instalação recebeu esse pacote. O
  processo validado ficou aberto ao final.

## Recorte lateral dos períodos — 2026-09-29

A captura da etapa anterior expôs uma falha que não deveria ter sido aceita:
os `RadioButton` herdaram a largura mínima do controle nativo, maior que cada
coluna de um quarto da grade. O conteúdo excedia a célula e era recortado à
direita, deixando os controles com aparência de `[ 30` em vez de `[ 30 ]`.

- O estilo `CompassoDurationButtonStyle` agora define `MinWidth="0"`, permitindo
  que cada controle respeite integralmente a largura da sua coluna.
- O aplicativo e o instalador foram recompilados pelo fluxo canônico com zero
  avisos e zero erros; o payload foi regenerado com 536 arquivos, teve os 535
  arquivos do app conferidos por SHA-256 e foi aplicado à instalação.
- O teste instalado `30 → 60 → 15` continuou aprovado em todas as etapas
  (`EXCLUSIVE_SELECTION=PASS`).
- A captura substituída em
  `docs/design/windows-shell/validation/add-time-selection-15.png` foi
  inspecionada após a instalação: os quatro períodos aparecem completos, com
  borda direita visível e os quatro cantos arredondados. A opção 15 permanece
  coral com texto branco e as demais claras com texto escuro.
- O processo instalado usado na validação permaneceu aberto ao final.

## Centralização vertical dos rótulos de período — 2026-09-29

A captura usada para validar o recorte ainda mostrava os rótulos encostados na
parte superior dos controles. A causa era o template vincular a posição do
conteúdo a `VerticalContentAlignment` sem o estilo definir esse valor; o
`RadioButton` manteve então o alinhamento vertical nativo inadequado para este
layout.

- `CompassoDurationButtonStyle` agora define explicitamente
  `VerticalContentAlignment="Center"`; o `ContentPresenter` do template já
  consome esse valor por `TemplateBinding`.
- Aplicativo, instalador e payload foram reconstruídos pelo fluxo canônico com
  zero avisos e zero erros; os 535 arquivos do aplicativo foram conferidos por
  SHA-256 e a instalação foi atualizada.
- O fluxo de seleção `30 → 60 → 15` continuou aprovado
  (`EXCLUSIVE_SELECTION=PASS`).
- A captura atualizada
  `docs/design/windows-shell/validation/add-time-selection-15.png` foi
  inspecionada após a instalação: os rótulos 15, 30, 60 e 120 estão no centro
  horizontal e vertical de seus controles, que permanecem inteiros e com os
  quatro cantos arredondados.
- A regressão completa de Adicionar tempo, Configurações e navegação terminou
  com `LastTaskResult=0`. O executável instalado permaneceu aberto e responsivo
  ao final.

## Contorno dos campos nas duas interfaces — 2026-09-29

O campo de senha de Adicionar tempo não correspondia ao conceito: o
`PasswordBox` estava sem estilo próprio e exibia o chrome Fluent padrão, com a
borda inferior enfatizada. Em Configurações havia ainda duas estratégias
diferentes: os `TextBox` usavam propriedades de borda do controle, enquanto o
token já usava um contorno externo.

A correção foi aplicada às duas interfaces vigentes do `CompassoApp`:

- criado `CompassoFieldBorderStyle`, responsável pelo fundo, contorno uniforme
  de 1 px e raio de 8 px;
- `TextBox` e `PasswordBox` agora ficam internamente transparentes e sem borda
  própria, preservando edição, placeholder e automação nativos;
- o campo de senha em Adicionar tempo passou a usar o contorno externo, com o
  olho de revelar mantido dentro dele;
- servidor, identificador e token em Configurações passaram a compartilhar o
  mesmo tratamento; o divisor e a ação `Mostrar` do token foram preservados;
- o `ComboBox` de conta não foi alterado porque já exibia o contorno uniforme
  previsto no conceito.

O fluxo canônico recompilou aplicativo e instalador com zero avisos e zero
erros, regenerou 536 arquivos no payload e conferiu os 535 arquivos do app por
SHA-256. O hash de `CompassoApp.dll` coincidiu entre payload e instalação
(`5D3C6740…A22BF555`).

Validação na instalação real:

- conteúdo, revelar/ocultar senha e token e navegação de ida e volta passaram
  (`CompassoVisualQa`, `LastTaskResult=0`);
- `verify-field-outline.ps1` colocou foco na senha de Adicionar tempo, no
  servidor e no token de Configurações e capturou os três estados:
  `FIELD_OUTLINE_FOCUS_CAPTURE=PASS`;
- a inspeção das capturas confirmou contorno fechado nos quatro lados e
  ausência da linha inferior tanto em repouso quanto com foco;
- `add-time-final.png` e `settings-final.png` foram substituídas pelas capturas
  da instalação atualizada. As capturas específicas de foco também ficaram em
  `docs/design/windows-shell/validation/`;
- o aplicativo instalado permaneceu aberto ao final da validação.

## Correções e validações — 2026-10-01

### Heartbeat real validado (item 4 do checklist, parcial)

- Build Windows não é possível neste Linux: não há cross-compiler com CGO e o
  `gccgo` disponível não suporta o backend Windows de `x/sys`. O build oficial
  continua sendo `windows/build-agent-service.ps1` na VM.
- `agent/cmd/compasso-agent-windows/main_windows.go` deixou de ser um esqueleto
  e passou a executar o mesmo encadeamento de `agent/cmd/tempo-agent/main.go`:
  configuração → storage → `BindEnrollment` → `session.NewWindows` →
  `daemon.New` → `syncclient` → `localauth` → daemon.
- Intervalos espelham o Linux: tick 1s, checkpoint 5s, HTTP 8s,
  heartbeat 3s (`syncclient.DefaultHeartbeatInterval`).
- `SetAlertNotifier` recebe um `Notifier` no-op explícito. O motor já tolera
  `nil` (`agent/daemon/daemon.go:319`); o no-op existe para manter a forma igual
  à do Linux enquanto as notificações nativas ficam pendentes.
- Novo comando `configure <server-url> <device-id> <SID>` grava a configuração
  pelo `windowsservice.SaveConfiguration` existente, que já aplica DPAPI e ACL.
  O token é lido de **stdin** para não aparecer na linha de comando nem no
  histórico do shell.
- Divergências deliberadas em relação ao Linux, todas por Windows: TOML vira
  JSON com DPAPI, `os/user` vira SID, `syscall.Umask` vira ACL, e não existe
  marcador de setup — a presença da configuração equivale a setup concluído,
  então `BindEnrollment` recebe `true`.
- `svc.Handler.Execute` agora executa o agente em goroutine e trata o erro: se o
  agente falhar ao subir, o serviço sai com erro em vez de declarar `running`.
  O `runConsole` passou a rodar o agente de verdade, o que permite validar sem
  instalar o serviço.

Validação na VM (`windows11\Sergio`, SID
`S-1-5-21-278194532-2139705530-887251162-1002`, confirmado como a única conta
real do computador):

- `CompassoAgent.exe` compilado: 22.165.938 bytes, SHA-256
  `11B1D261850DD29B4B8B24E1C9E4C4F6D9F994A02EEDCC51B726EFDF7F55E508`.
- Configuração gravada em `C:\ProgramData\Compasso\agent-config.json` com
  `has_device_token: true`, token protegido por DPAPI e diretório com ACL.
- Execução em modo console por 25 s contra `https://apifamily.smresume.com`:
  ```
  starting controlled_user_sid=S-1-5-21-…-1002 database=C:\ProgramData\Compasso\agent.db
  synchronization enabled server=https://apifamily.smresume.com device_id=0e2b55e7-…-c79224
  agent cycle failed: no local policy
  synchronization online
  agent cycle recovered
  decision=awaiting_synchronization session=true usage_seconds=0 remaining_seconds=0
  ```
- `synchronization online` prova a cadeia completa ponta a ponta: leitura de
  config com DPAPI, abertura do store, `BindEnrollment`, TLS até o servidor,
  cabeçalhos, identidade de instalação e aceitação das credenciais do
  dispositivo.
- `no local policy` seguido de `agent cycle recovered` é o comportamento
  correto do primeiro ciclo, antes de existir política. Não é falha.
- O endpoint responde `invalid_device_credentials` a sondagens sem token, o que
  confirma que a autorização é exigida de fato.

### Pendências do item 4

- Aplicar uma política pelo painel e observar o serviço Windows consumindo e
  reportando.
- Confirmar um comando recebido do servidor.
- Forçar perda de conexão e observar a recuperação.

### Decisões de arquitetura sobre notificações

- Regra acordada: preservar o **comportamento** do Linux; diferenças internas
  são permitidas quando justificadas pelo sistema operacional e quando reduzem
  código especial.
- O Linux **não tem** agente residente na sessão do usuário. `agent/alert/desktop.go`
  faz o serviço root executar, sob demanda,
  `systemd-run --user --machine=<usuário>@.host --collect --quiet notify-send`,
  ou seja, um processo curto dentro da sessão gráfica, que termina em seguida.
- Para o Windows, foi descartado o espelho literal. `CreateProcessAsUser` cria o
  processo na sessão alvo, porém herda a área de trabalho do pai, que na
  Session 0 é `Service-0x0-…$\Default` e não `WinSta0\Default`. O helper então
  roda fora da sessão gráfica e o toast **falha em silêncio, sem erro**.
  Corrigir exige `STARTUPINFO.lpDesktop = "WinSta0\Default"`.
- Alternativa escolhida: **agente Windows residente na sessão do usuário**,
  concentrado as operações que dependem do desktop — notificações,
  `LockWorkStation`, interface de Adicionar tempo e comunicação com o
  configurador. O serviço segue responsável por regras, tempo, política e
  decisões privilegiadas.
  ```
  Serviço Compasso -> IPC -> Agente da sessão -> operação no desktop
  ```
- Consequências aceitas e que precisam ser implementadas: ciclo de vida próprio
  do agente (subir com a sessão, reiniciar se morrer); degradação sem ele sem
  quebrar contagem nem política; o serviço trata entrada do agente como não
  confiável, com conjunto mínimo de comandos validados; e o agente não pode
  encerrar ao fechar a janela da interface (modo bandeja).
- O IPC é fronteira de privilégio: serviço LocalSystem e agente de integridade
  média. Atenção ao precedente já pago — o instalador registrou que servidor
  elevado com cliente não elevado falha sob `CurrentUserOnly`; aqui o caso é
  justamente esse, então o padrão do instalador não pode ser reutilizado às
  cegas.
- Toast para app *unpackaged* exige AUMID registrado por atalho no menu Iniciar.
  Deve ser reaproveitado o AUMID do Compasso, que o instalador já registra, em
  vez de criar um novo.
- **Não reabrir a decisão do bloqueio:** `WTSDisconnectSession` foi validado a
  partir da Session 0 e o código original de `agent/session/windows.go` está
  correto. A existência do agente não obriga trocar por `LockWorkStation`.

### Próximo passo concreto

Aplicar uma política de tempo pelo painel para o dispositivo Windows
`0e2b55e7-4104-4f56-9d4f-7cb702c79224` e observar, no log do serviço, o consumo
sendo reportado ao servidor.

### Item 4 concluído — cenários validados pelo usuário (2026-10-01)

O serviço foi instalado de verdade na VM (`CompassoAgent`, início automático
atrasado, `Running`, estado `running` em `service-state.json`), então toda a
validação abaixo ocorreu pelo caminho de produto: Session 0, LocalSystem.

- Política publicada pelo painel e recebida pelo agente: revisão 7, com
  `weekly_quota` por dia da semana e `warning_minutes=10`.
- Cenários confirmados interativamente pelo usuário na máquina real:
  - bloqueio ao esgotar o tempo;
  - retomada ao adicionar mais tempo;
  - bloqueio manual;
  - desbloqueio manual com senha na sessão do usuário;
  - pausa da monitoração;
  - pausa sem contagem de tempo.
- `LockWorkStation()` permaneceu fora do caminho do serviço, como decidido:
  o bloqueio efetivo é `WTSDisconnectSession`, e o destravamento é sempre
  manual. Não houve tentativa de desbloqueio remoto em nenhum cenário.

### Ferramenta de diagnóstico adicionada

- Novo comando `compasso-agent inspect-state`, somente leitura via API do
  `agent/storage`, que informa política vigente, uso diário, bônus, estado de
  sessão confirmado, eventos pendentes e comandos aplicados. Existe porque o
  SCM descarta a saída do serviço, então o `agent.db` é a única fonte estável
  de verdade durante a validação.
- Ferramentas de teste fora do repositório: `watch.ps1` e `watch2.ps1` na VM,
  usados só para observação. Nada disso foi versionado.

### Dívidas conhecidas, não tratadas

- Notificações nativas do Windows: agreed a arquitetura (agente residente na
  sessão), nada implementado. O ponto de extensão é o `Notifier` no-op em
  `agent/cmd/compasso-agent-windows/main_windows.go`.
- `go test ./agent/...` no Windows agora passa com CGO habilitado (MSYS2 UCRT64).
  Os três pacotes Linux-only (`agent/cmd/tempo-agent`, `agent/setup`,
  `agent/syncstatus`) foram restritos a `//go:build !windows`, portanto o build
  Windows ignora seus arquivos e o Linux continua testando-os integralmente.
  Também corrigido `agent/session/windows.go` para evitar construções inseguras
  de ponteiros detectadas por `go vet` no Windows.
- Build Windows continua dependente da VM: não há cross-compiler com CGO neste
  Linux e o `gccgo` disponível não suporta o backend Windows de `x/sys`.
- Rejeição de sessão RDP real continua coberta apenas por teste unitário.

### Próximo passo

Item 5: IPC local protegido e conexão da tela **Adicionar tempo**. O serviço já
está com política, sessão e bloqueio funcionando, então falta expor a operação
privilegiada de bônus com senha, incluindo rate limit e evento durável.

## Item 5 — IPC local protegido e Adicionar tempo

Servidor (`agent/windowsipc`), cliente (`windows/CompassoWails/ipcclient`) e
ligações da tela **Adicionar tempo** implementados. Isso ainda não constitui
validação de ponta a ponta pela interface Wails real. Pipe em
`\\.\pipe\CompassoAgent`, enquadramento `uint32` little-endian + JSON, limite de
64 KiB, uma requisição por conexão.

### Causa raiz do `Parâmetro incorreto`

`PIPE_REJECT_REMOTE_CLIENTS` é rejeitada por este build (10.0.26200) com
`ERROR_INVALID_PARAMETER`. Medido em Go e nativo, em todas as combinações de
descritor de segurança e contagem de instâncias, inclusive com `FILE_FLAG_OVERLAPPED`
e com descritor nulo. A flag foi removida e o DACL passou a ser o controle:
um token autenticado pela rede nunca é LocalSystem, administrador ou a conta
controlada.

### Quatro defeitos de lifecycle encontrados depois

A criação do pipe deixou de ser o único problema. Os testes e2e novos expuseram:

1. **Contagem de bytes do I/O overlapped.** Em `ReadFile`/`WriteFile` que não
   completam de imediato, a contagem só vem em `GetOverlappedResult`. Ler a
   variável deixada por `ReadFile` produzia `0` e um `io.EOF` falso, fechando a
   conexão sem responder. Era intermitente: o `ping` curto quase sempre
   completava de imediato e passava.
2. **Handle fechado duas vezes.** `Conn.Close` fechava o handle e o `Listener`
   mantinha o valor obsoleto. O Windows reutiliza valores de handle, então o
   segundo fechamento derrubava a instância criada para o cliente seguinte.
3. **Corrida de startup.** `Listen` criava a instância e o primeiro `Accept` a
   substituía; um cliente que conectasse nesse intervalo perdia o pipe.
4. **Saída silenciosa do laço.** Qualquer erro desconectado fazia `Serve`
   retornar `nil`, deixando o pipe vivo sem ninguém aceitando: os clientes
   seguintes travavam e quebravam.

O serviço silenciosamente sem interface foi o sintoma que expôs o ponto 4: o
SCM seguia `Running` com um binário antigo, e nada no log indicava falta de pipe.

### Endurecimento

- `GA` da conta controlada passou a `GRGW`. `GA` inclui
  `FILE_CREATE_PIPE_INSTANCE`, que permitiria a um processo da conta controlada
  criar instâncias do pipe.
- `FILE_FLAG_FIRST_PIPE_INSTANCE` na primeira instância: a criação falha se o
  nome já existir, impedindo sequestro do nome, captura da senha do responsável
  e resposta forjada de bônus.
- `serveLocalInterface` reconstrói o pipe se a interface parar, sem reiniciar o
  serviço. A política e o bloqueio continuam ativos enquanto a interface cai.

### Validação técnica na VM (parcial)

- `ping` 10/10 sequenciais; suíte e2e com 12 requisições e operações
  intercaladas; cliente com 15 requisições.
- Senha incorreta → `invalid_password`; repetição imediata → `rate_limited`;
  operação desconhecida → `invalid_request`.
- Senha correta → `ok`, UUID durável, `bonus_seconds=900`, `total_seconds=900`,
  `remaining_seconds` de 4800 para 5700 e evento já sincronizado
  (`pending_events` vazio).

Conta de testes: a execução concedeu dois bônus de 15 minutos em vez de um,
porque o teste de aceite supunha que um bônus concedido também seria limitado
por taxa. Não é: `localauth` conta apenas tentativas **falhadas**. Registrado
como teste em `agent/localauth/service_test.go`
(`TestSuccessfulGrantIsNotRateLimited`), porque é a diferença entre proteger
contra tentativa repetida e travar o usuário legítimo.

### Limite da evidência e reabertura — 2026-10-02

A evidência acima exercitou requisições IPC, o cliente e o estado persistido no
banco, mas não registrou os cenários acionados pela tela real da interface
vigente `windows/CompassoWails`. A afirmação anterior de ligação "de ponta a
ponta" era, portanto, mais forte que a evidência disponível. O item 5 permanece
parcial e foi reaberto no checklist.

Depois da reabertura, a interface vigente foi ligada ao serviço, incorporada ao
instalador e exercitada instalada na VM. Foram confirmados pela tela o estado do
serviço, a escolha do período, a senha incorreta, o rate limit, o serviço
indisponível e a limpeza da senha. A interface só aceita sucesso quando a
resposta contém o evento e o bônus persistidos com UUID correspondente, usando
o mesmo motor local do Linux.

O item 5 continua parcial porque a validação de aceite ainda não foi encerrada.
A única pendência é de **testes**, incluindo executar pela interface instalada o
sucesso com a senha real e conferir o bônus, o evento e o saldo persistidos. Não
há lacuna funcional conhecida nem impedimento para iniciar o item 6.

### Divergência conhecida

O módulo Wails é standalone e não importa `agent/windowsipc`, porque
`localauth` puxa `agent/storage` e CGO/sqlite, que não pertencem a um
instalador de interface. O contrato do wire é duplicado em
`windows/CompassoWails/ipcclient/protocol.go`, com o canônico em
`agent/windowsipc/protocol_windows.go` apontado no comentário do pacote.

`get_public_configuration` já está exposto no servidor para alimentar a tela de
Configurações, mas a tela ainda não grava configuração: isso é o item 6.

### Dívidas conhecidas, não tratadas

- Notificações nativas: arquitetura acordada (agente residente na sessão), nada
  implementado. Ponto de extensão é o `Notifier` no-op em
  `agent/cmd/compasso-agent-windows/main_windows.go`.
- `go test ./agent/...` no Windows falha em três pacotes Linux-específicos:
  resolvido — ver seção "Higiene de build entre plataformas".
- Build Windows continua dependente da VM: sem cross-compiler com CGO neste
  Linux, e o `gccgo` disponível não suporta o backend Windows de `x/sys`.
- Rejeição de sessão RDP real continua coberta apenas por teste unitário.

## Higiene de build entre plataformas

`go test ./agent/...` no Windows falhava em três pacotes. Nenhum era bug de
lógica: eram três erros de classificação de plataforma.

- `agent/cmd/tempo-agent`: usa `syscall.Umask` e `session.NewLogind`, ambos
  inexistentes no Windows. Era falha de *build*, não de teste.
- `agent/setup`: `WriteFileAtomically` faz `fsync` no diretório pai, e o Windows
  recusa `FlushFileBuffers` em handle de diretório.
- `agent/syncstatus`: o teste afirma bits POSIX `0600`, que não existem no
  Windows.

Os três são ferramentas do agente Linux, e o caminho equivalente no Windows já
existe pelo serviço e pela tela. `agent/cmd/tempo-agent-configure` também foi
restringido, porque importa `setup` e `syncstatus` e fixa `/etc/tempo-agent` e
`/usr/bin/systemctl`.

O critério não foi "fazer passar", mas "não fingir": os três pacotes prometem
garantias POSIX reais — arquivo `0600`, recusa de symlink, diretório
`root-only` — que no Windows não têm tradução honesta. Torná-los "portáteis"
significaria remover as asserções que protegem a configuração no Linux para
ganhar uma linha verde numa plataforma onde nada os executa.

Verificado antes de agir: o `./...` do Go **ignora silenciosamente** diretórios
cujos arquivos estão todos excluídos por build constraint, então a restrição não
exige stubs e não polui a saída do build.

O problema espelho também existia: `agent/cmd/compasso-agent-windows/doc.go` não
tinha build tag, então no Linux o pacote era um `main` sem `main()` e
`go build ./...` falhava na linkagem com `undefined reference to "main.main"`.
Corrigido com `//go:build windows`, seguindo a convenção já usada em
`agent/session/loginctl.go` e `windows.go`.

### CGO é obrigatório para testar o agente no Windows

Depois de resolver os três pacotes, a suite ainda falhava inteira em
`agent/syncclient` com `Binary was compiled with 'CGO_ENABLED=0', go-sqlite3
requires cgo to work`. Não é bug do Windows: o agente depende de
`mattn/go-sqlite3`, e `go env CGO_ENABLED` é `0` por padrão na VM porque o gcc do
MSYS2 só entra no `PATH` pelo script de build.

Em vez de espalhar `t.Skip` pelos testes que dependem de SQLite — o que
esconderia regressões reais — foi criado `windows/test-agent.ps1`, que replica o
setup de CGO de `build-agent-service.ps1`. Pular testes deixaria a suite verde
sem testar nada, e o agente não funciona no Windows sem CGO de qualquer forma.

Resultado no Windows, com CGO habilitado: `go vet ./...`, `go build ./...` e
`go test ./agent/...` com exit 0, doze pacotes verdes. No Linux: `make lint` e
`go test ./...` limpos, com os quatro pacotes Linux-only ainda testados.

### Ponteiros inseguros em `agent/session/windows.go`

`go vet` no Windows apontou `possible misuse of unsafe.Pointer` na consulta WTS.
Era um padrão real, não só cosmético: o buffer devolvido por
`WTSQuerySessionInformation` via `uintptr` era convertido de volta para ponteiro
no consumidor, com aritmética de ponteiro feita em espaço `uintptr`.

Esse é o problema documentado de `unsafe`: converter `uintptr` de volta para
`unsafe.Pointer` é inválido sob um coletor de lixo móvel, porque o coletor pode
mover a alocação original entre a conversão e a leitura. `queryWTSInformation`
agora devolve `unsafe.Pointer` de ponta a ponta e a leitura do deslocamento usa
`unsafe.Add`.

A mudança preserva comportamento, mas elimina um risco concreto: o serviço
desserializa o estado de sessão para decidir se desconecta o usuário.

Regra do agente: como vet e testes rodam no Linux com build tags, `agent/session`
e `agent/windowsipc` só são exercitados no Windows. Build e testes Windows devem
ser executados na VM para cobrir esses arquivos.

## Item 6 — Configurações funcional e instalador atualizado — 2026-10-03

O escopo vigente foi confirmado antes da alteração: a interface Windows é
`windows/CompassoWails`. Os projetos WinUI históricos `windows/CompassoApp` e
`windows/CompassoInstaller` não receberam cópia da implementação. A referência
funcional foi o assistente Linux em `local-ui/configure_agent.py` e o helper
privilegiado `agent/cmd/tempo-agent-configure`.

### Fluxo entregue

- A tela enumera contas locais normais e habilitadas, põe a conta corrente
  primeiro e mantém nome + SID. Trocar a conta invalida a confirmação anterior.
- A interface valida origem HTTP(S), exige HTTPS fora de loopback e exige
  identificador, token e confirmação explícita da conta.
- `Configure` cria um pipe de uso único, com nonce aleatório e DACL limitada a
  administradores/SYSTEM, e abre o próprio agente instalado com `runas`.
  Token, identificador e URL não entram em argumentos do processo.
- O helper elevado revalida de forma autoritativa a relação nome/SID e confirma
  que a conta é local, comum e habilitada. A configuração é gravada com DPAPI e
  ACL já usadas pelo serviço Windows.
- Antes de cada tentativa, `setup-complete` é removido. O serviço é configurado
  como automático, reiniciado e consultado pelo IPC por até 30 segundos. O
  marco durável só é escrito depois de um heartbeat `online`; 401/403 são
  apresentados como credencial recusada. O token é esvaziado na tela em todo
  resultado e nunca volta pelo IPC público.
- Quando existe configuração, mas ainda não existe o marco, sincronização e IPC
  ficam disponíveis para concluir o assistente, porém o daemon de política não
  inicia. Isso impede bloqueio antes da primeira configuração confirmada.
- A abertura `--settings` usada pelo instalador inicia diretamente a tela de
  configuração. O estado de serviço/servidor e o estado configurado são
  atualizados continuamente, inclusive se o serviço ainda estava subindo na
  primeira leitura.

### Instalador

`windows/build-portable-installer.ps1` agora recompila o serviço e o Wails antes
de montar o artefato. `CompassoSetup.iss` instala `CompassoAgent.exe`, registra
o serviço, inicia-o somente se já houver configuração e abre Configurações para
o usuário original depois de uma instalação interativa. A desinstalação para e
remove o serviço.

Durante o teste de atualização foi encontrada uma falha concreta: a limpeza
antecipada de `{app}` podia apagar arquivos antes de uma atualização terminar,
e o Restart Manager não conseguia fechar o Wails quando instalador e aplicação
estavam em sessões interativas diferentes. A limpeza destrutiva foi removida e
`PrepareToInstall` agora fecha `Compasso.exe` explicitamente antes de parar o
serviço. A atualização com a interface aberta passou depois da correção.

### Evidência na VM Windows 11

- `windows/test-agent.ps1`: doze pacotes do agente verdes, incluindo helper,
  `windowsipc` e `windowsservice`.
- `go test ./...` em `windows/CompassoWails`: módulo principal e cliente IPC
  verdes.
- Testes do helper cobrem nonce, conta local corrente, resposta sem token,
  mensagens sanitizadas, primeiro heartbeat aceito e rejeição de credenciais.
- Fluxo real instalado: conta `Sergio`, confirmação, credencial válida, UAC,
  gravação, reinício do serviço, heartbeat, `setup-complete` e mensagem de
  sucesso. Na captura posterior o chip está **Configurado**, o servidor está
  **conectado** e o campo de token está vazio.
- Reinício do serviço seguido por `go test -count=1 -v ./ipcclient`: ping,
  sincronização, configuração pública sem token, requisições repetidas, erro de
  senha e indisponibilidade passaram.
- Atualização silenciosa com `Compasso.exe` aberto: exit code `0`, serviço
  `Running`, configuração preservada e binários presentes.
- Ciclo final do instalador: serviço removido na desinstalação; app, agente e
  atalhos removidos; reinstalação com serviço `Running`/`Auto`; cinco arquivos
  instalados e nenhum runtime .NET embutido.

Artefato final: `windows/artifacts/CompassoSetup.exe`, 11.417.231 bytes,
SHA-256 `CE240B582B1F8DCBC03AA705FAE83C67123B739A2303AA67AF75BE8572B166C0`.

O item 6 está concluído. O item 7 permanece aberto porque seu critério exige um
aceite adicional em VM realmente limpa e reinício completo do Windows, embora
inclusão no instalador, atualização e remoção do serviço já tenham sido
exercitadas.

## Retomada do item 7 — aceite do novo instalador — 2026-10-03

O usuário confirmou que a entrega principal desta etapa é o novo instalador.
A tela de Configurações permanece como dependência já implementada e não será
redesenhada. A única interface vigente continua sendo
`windows/CompassoWails`; os projetos WinUI históricos permanecem fora do
escopo.

Estado autoritativo observado antes do aceite: a VM `win11` está ativa, a
instalação e o desinstalador existem, `%ProgramData%\Compasso` contém
configuração e `setup-complete`, e o serviço `CompassoAgent` está `Running`
com início automático. Esse estado confirma a instalação atual, mas não serve
como evidência de primeira instalação limpa.

Objetivo da etapa longa: reconstruir o artefato vigente, preservar o estado
atual por um mecanismo recuperável e executar em uma VM sem instalação nem
configuração preexistentes o ciclo instalar → configurar → reiniciar o
Windows → atualizar → desinstalar. O aceite deve verificar serviço,
atalhos, binários, estado durável e ausência de resíduo executável, sem expor
ou tentar recuperar o token protegido.

### Build e primeira instalação limpa

- O espelho da VM foi conferido por SHA-256 contra os arquivos críticos do
  worktree. Serviço, Wails e Inno Setup foram reconstruídos pelo fluxo canônico.
  Artefato: 11.417.231 bytes, SHA-256
  `A7D1B721E5A1D77B44F161C5EE3A20B898834435F7996B2E24E763D4C58E8601`.
- Testes Windows: treze pacotes do agente e os pacotes `Compasso` e
  `Compasso/ipcclient` verdes. Os pacotes compartilhados do agente e protocolo
  também passaram no Linux. O Go local não aceita a versão `1.25.0` declarada
  pelo módulo Wails; a suíte correspondente passou com Go 1.27 na VM.
- Foi criado o snapshot offline `pre-clean-installer-20261003`. A instalação
  anterior foi removida e `%ProgramData%\Compasso` foi movido para
  `C:\ProgramData\Compasso.acceptance-20261003-143239`, sem tentar ler o token
  protegido.
- `windows/verify-wails-clean-install.ps1` registrou
  `CLEAN_PRECONDITION=PASS` e `CLEAN_INSTALL=PASS`: interface, agente,
  desinstalador e atalhos presentes; binários instalados idênticos à build;
  nenhum runtime .NET, configuração ou `setup-complete` herdado.
- A primeira asserção esperava início automático antes da configuração. A
  comparação com `tempo-agent-configure` mostrou que o Linux só habilita o
  serviço depois de receber a configuração. O estado Windows medido,
  `Manual`/`Stopped`, é deliberado; o verificador e o contrato foram corrigidos
  para exigir `Auto`/`Running` depois da configuração.

Próximo passo: entrar na sessão gráfica da VM, configurar a instalação limpa
pela interface vigente com uma credencial real e continuar as fases
`configured`, `post-reboot`, `update` e `uninstall` do verificador.

## Diagnóstico de desinstalação e persistência — 2026-10-03

Diagnóstico solicitado sem aplicar correção. Depois dos ciclos manuais, o
estado autoritativo da VM era: serviço `CompassoAgent` ausente, nenhum processo
Compasso ativo, registro de desinstalação ausente e somente
`C:\Program Files\Compasso\Compasso.exe` restante na pasta do produto. Isso
isola o resíduo executável da interface que estava aberta durante a
desinstalação; o serviço foi removido corretamente.

### Processo que mantém o executável bloqueado

O instalador vigente encerra `Compasso.exe` explicitamente apenas em
`PrepareToInstall`, portanto no caminho de instalação/atualização. O caminho
de desinstalação executa somente `CompassoAgent.exe uninstall`, aguarda a
remoção do serviço e tenta apagar `{app}`. Não existe hoje uma etapa explícita
para encerrar e aguardar a interface Wails durante o uninstall. Com a interface
aberta, o próprio processo `Compasso.exe` mantém sua imagem mapeada; a pasta é
removida parcialmente e esse arquivo permanece.

Os executáveis/processos relevantes são: `Compasso.exe` (interface),
`CompassoAgent.exe service` (serviço) e, transitoriamente,
`CompassoAgent.exe configure-ui <nonce>` (helper elevado). O WebView2 também
cria processos `msedgewebview2.exe`, mas a evidência desta falha é o processo
principal aberto e o único resíduo foi sua própria imagem. O serviço já é
parado e removido pelo comando de uninstall.

### Origem exata da configuração e identidade

O desinstalador vigente remove `{app}`, atalhos, serviço e registro de
desinstalação, mas não referencia `%ProgramData%\Compasso`. O estado preservado
medido na VM foi:

- `agent-config.json`: URL, `device_id`, SID da conta controlada e token
  protegido por DPAPI de máquina (`device_token_dpapi` presente, sem revelar o
  valor);
- `agent.db` e seus arquivos WAL/SHM: política, uso, filas e a inscrição local;
  a linha de enrollment preserva URL, `device_id`, UUID de instalação de 36
  caracteres e impressão SHA-256 do token de 64 caracteres;
- `setup-complete`: confirmação durável `configured`;
- `service-state.json`: último estado de ciclo de vida do serviço.

Na reinstalação, `installService` encontra `agent-config.json`, registra o
serviço como automático e `start-if-configured` o inicia. O agente abre o mesmo
`agent.db`, reaproveita a identidade da mesma combinação servidor/dispositivo/
impressão do token e envia ao servidor o mesmo `device_id`, token e
`installation_id`. O servidor apenas autentica esses valores e recusa um UUID
de instalação divergente; não há reconhecimento automático por hardware,
Windows, MAC ou nome da máquina.

Em perfil de usuário existem `AppData\Roaming\Compasso.exe`, usado pelo perfil
WebView2, e `AppData\Local\Compasso\startup.log`, originado pela implementação
WinUI histórica. A interface Wails vigente não usa `localStorage` para a
configuração e sempre consulta o serviço por IPC. Nenhuma chave Compasso foi
encontrada nas raízes de software HKCU/HKLM depois do uninstall.

### Origem de `Compasso.new`

`C:\Program Files\Compasso.new` é criada exclusivamente pelo
`InstallPlanExecutor` de `windows/CompassoInstaller`, o instalador WinUI
histórico: ele monta a árvore nova nesse diretório, move a instalação anterior
para `.previous` e ativa a nova árvore. `CompassoSetup.iss`, vigente, não cria
nem referencia `.new`. A pasta observada era resíduo de uma execução histórica
e sua remoção não poderia alterar a configuração em ProgramData.

### Decisão de produto ainda necessária

A política implementada hoje é **preservar configuração, identidade e estado
local no uninstall**, o que explica a reconexão automática depois de reinstalar.
Ainda deve ser decidido se o produto manterá essa política, removerá todo o
estado local, ou oferecerá uma escolha explícita entre desinstalação comum e
remoção completa. Nenhuma dessas alternativas foi implementada nesta etapa.

## Correção do uninstall completo — 2026-10-03

A política definida passou a ser remoção completa no uninstall. O instalador
vigente executa `taskkill /F /T /IM Compasso.exe` de forma síncrona antes de
remover o serviço e inclui `C:\ProgramData\Compasso` em `UninstallDelete`.
Instalação, atualização e reparo não removem esse diretório.

Validação localizada na VM Windows:

- instalação por cima de uma configuração existente preservou integralmente
  os hashes de `agent-config.json` e `setup-complete`, além do mesmo
  `device_id`; o serviço permaneceu em execução;
- com `Compasso.exe` aberto na sessão interativa, o uninstall terminou com
  código 0, encerrou o processo e não deixou `Compasso.exe`, diretório de
  instalação, serviço ou `C:\ProgramData\Compasso`;
- a instalação seguinte não encontrou `agent-config.json` nem
  `setup-complete` e registrou o serviço como `Manual`/`Stopped`, confirmando o
  retorno ao estado inicial de configuração.

O artefato regenerado tem 11.418.468 bytes e SHA-256
`FA1FE4E30C1417FF7CC94CC7BF9D1D66722848E7ED61B6D59ACB3C38CA2C7E84`.
