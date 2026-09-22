# Instalação do agente no Windows

O pacote `CompassoAgent-<versão>-windows-x64.exe` é um instalador offline para
Windows 11 x64. Ele contém o serviço e o companion; a instalação não baixa
componentes adicionais.

## Instalação

1. No painel administrativo, crie o dispositivo e gere seu token.
2. Execute o instalador como administrador.
3. Informe a conta Windows que receberá as políticas. Pode ser usado o nome
   local, como `Sergio`, ou o formato `COMPUTADOR\Sergio`.
4. Confirme o endereço do servidor, o ID e o token do dispositivo.

O instalador grava os executáveis em `%ProgramFiles%\Compasso\Agent`, guarda
configuração, banco e logs em `%ProgramData%\Compasso\Agent`, restringe os
dados a `SYSTEM` e Administradores e registra o serviço automático
`CompassoAgent`.

## Atualização

Execute o instalador da nova versão. A opção padrão preserva a configuração e
o banco existentes, para manter a identidade da instalação e o estado offline.
O instalador para o serviço, troca os binários e volta a iniciá-lo.

Use a opção de substituir a configuração somente ao trocar o dispositivo, o
servidor ou a conta controlada.

## Remoção

Use **Aplicativos instalados > Compasso Agent > Desinstalar**. O assistente
pergunta se configuração e histórico local devem ser preservados.

Em automação, `/PURGEDATA` remove explicitamente esses dados junto com o
programa:

```powershell
& "$env:ProgramFiles\Compasso\Agent\unins000.exe" /VERYSILENT /PURGEDATA
```

## Build

Em Linux, gere os binários Windows com:

```bash
make build-agent-windows
```

Em uma máquina Windows com Inno Setup 6, a partir da raiz do repositório:

```powershell
.\scripts\build-windows-installer.ps1
```

O instalador completo será criado em `dist`.
