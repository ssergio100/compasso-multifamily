# Build Windows vigente

O artefato distribuível vigente é `artifacts/CompassoSetup.exe`. Ele instala
somente a interface Windows do Compasso, implementada em `CompassoWails`.

O instalador contém um único aplicativo Go/Wails e não transporta .NET nem o
Windows App SDK. O WebView2 já presente no Windows é usado pela interface e não
é duplicado dentro do pacote.

Os projetos `CompassoApp` e `CompassoInstaller` são históricos. Não os compile,
empacote ou distribua como implementação vigente: ambos eram self-contained e
duplicavam runtimes no pacote.

## Gerar o instalador

No Windows, execute:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\build-portable-installer.ps1
```

O script:

1. compila a interface Wails para `Compasso.exe`;
2. gera `artifacts/CompassoSetup.exe` com Inno Setup;
3. exige que a pasta de entrega contenha somente esse `.exe`;
4. imprime tamanho e SHA-256 do artefato final.

Dependências de build: Go, Node.js, Wails v2 e Inno Setup 6. Elas são usadas
somente para compilar e não são instaladas no computador do usuário.

## Validar

Em um PowerShell elevado:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\verify-wails-install-cycle.ps1
```

O teste parte tanto de uma máquina limpa quanto de uma instalação anterior,
desinstala, reinstala, confere os atalhos e rejeita DLLs ou arquivos de runtime
.NET no diretório instalado.

`verify-portable-installer-ui.ps1` percorre o assistente na sessão gráfica e
confirma que a interface instalada abre ao concluir.

Build validada em 30/09/2026:

- `CompassoSetup.exe`: 5.660.559 bytes (5,40 MiB);
- `Compasso.exe`: 10.488.832 bytes instalado;
- quatro arquivos instalados: aplicativo, ícone versionado e dois arquivos do
  desinstalador;
- zero arquivos de runtime .NET;
- SHA-256: `508A3485B7CC0FB0CE0DDD88260CE365E6102C6E854280F042500A7880575DD8`;
- Microsoft Defender: nenhuma ameaça encontrada.

O executável ainda não possui assinatura digital de distribuição.
