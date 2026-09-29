# Windows — log de portabilidade

Este é o ponto único de retomada do trabalho. Registre aqui apenas fatos
verificados, decisões vigentes e o próximo passo concreto.

## Estado atual — 2026-09-29

- Primeira implementação vigente: `CompassoInstaller`, um instalador nativo em
  WinUI 3/C#. O instalador deve existir e ser validado antes das demais
  interfaces Windows.
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
- O repositório ainda contém, como alterações locais não commitadas, a reversão
  integral da tentativa Windows anterior. Essas alterações devem ser
  preservadas.

## Decisões vigentes

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

## Próximo passo único

Implementar estados explícitos de preparação e erro e conectar uma verificação
real de elevação administrativa. O fluxo não deve indicar instalação concluída
nem copiar componentes enquanto serviço e interfaces ainda não existirem.

## Problemas conhecidos

- A execução direta iniciada por SSH ocorre na sessão não interativa `0` e
  falha em `Microsoft.UI.Input.dll`; a validação de UI precisa ser iniciada na
  sessão interativa do usuário, como foi feito neste teste.
- A captura tradicional de tela não registra o conteúdo composto por GPU do
  WinUI nessa sessão remota. A existência e o conteúdo da janela foram
  verificados por HWND e UI Automation; a avaliação visual humana ainda é
  necessária.
