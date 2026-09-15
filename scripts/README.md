# Scripts do Compasso

Este diretório reúne scripts de desenvolvimento, empacotamento, publicação e
operação do servidor. Os arquivos menores permanecem separados para que possam
ser executados e testados isoladamente; os fluxos que normalmente ocorrem em
sequência possuem pontos de entrada consolidados.

## Pontos de entrada recomendados

Execute estes alvos a partir da raiz do repositório:

| Objetivo | Comando |
| --- | --- |
| Executar todas as verificações locais | `make test` |
| Validar links locais da documentação | `make test-docs` |
| Gerar o pacote Debian do cliente | `make package-deb` |
| Gerar e validar o pacote Debian do cliente | `make test-deb` |
| Gerar e validar cliente e servidor com a mesma versão | `make package-all` |
| Gerar o pacote Debian do servidor | `make package-server` |
| Validar um pacote existente do servidor | `make test-server-package` |
| Gerar, validar e publicar o agente para download | `make publish-client-release` |
| Gerar, validar e publicar o servidor | `make publish-server` |
| Compilar e publicar a interface administrativa | `make publish-admin-ui` |

`make package-all` incrementa o sufixo `~pilotN` em
`packaging/debian/control`. Essa alteração é intencional e permanece no
diretório de trabalho para ser incluída no commit da entrega.

## Fluxos e interdependências

### Entrega completa dos pacotes

```text
make package-all
  -> build-all-debian-packages.sh
       -> build-portable-client-binaries.sh
       -> build-debian-package.sh
       -> build-server-package.sh
       -> test-debian-package.sh
       -> test-server-package.sh
```

O fluxo só termina com sucesso depois que os dois arquivos `.deb` foram
inspecionados. Os artefatos e seus arquivos `.sha256` ficam em `dist/`.

### Pacote do cliente

```text
make test-deb
  -> make package-deb
       -> build-portable-client-binaries.sh
       -> build-debian-package.sh
  -> test-debian-package.sh
```

O build portátil usa Docker para produzir os executáveis esperados pelo pacote.
O teste do pacote é separado para também aceitar um `.deb` já existente.

### Publicação do servidor

```text
make publish-server
  -> publish-server.sh
       -> testes Go do servidor e protocolo
       -> build-server-package.sh
       -> test-server-package.sh
       -> envio e validação do checksum
       -> instalação remota do pacote
            -> install-server.sh, na primeira instalação
            -> update-server.sh, nas atualizações
                 -> backup-server.sh
                 -> status-server.sh
       -> healthcheck externo
```

Use `publish-server.sh --build-only` para executar somente a parte local. As
opções e variáveis de implantação estão documentadas por
`publish-server.sh --help` e em `docs/atualizacao-manual-servidor.md`.

### Verificação geral do repositório

```text
make test
  -> formatação e vet do Go
  -> testes Go
  -> testes da interface local
  -> typecheck e build da admin-ui
  -> test-migrations.sh
  -> test-security-packaging.sh
  -> build dos binários Go
```

## Referência de cada script

### Desenvolvimento e pacotes

#### `build-portable-client-binaries.sh`

Compila `tempo-agent` e `tempo-agent-configure` no ambiente Docker de build,
verifica dependências dinâmicas e instala os executáveis em `bin/`. É uma etapa
obrigatória antes de montar o pacote do cliente.

#### `build-debian-package.sh`

Monta o `.deb` do cliente a partir dos binários em `bin/`, das interfaces
locais, das unidades e políticas do sistema e da documentação. Gera também o
arquivo `.sha256` em `dist/`. Não compila os binários por conta própria.

#### `test-debian-package.sh`

Inspeciona o `.deb` do cliente sem instalá-lo. Valida metadados, dependências,
permissões, scripts mantenedores, binários, integração gráfica e ausência de
componentes legados. Aceita opcionalmente o caminho de um pacote existente.

#### `build-server-package.sh`

Monta o `.deb` independente de arquitetura do servidor. Inclui o código
necessário para construir a imagem da API, o Compose, a configuração inicial,
a documentação operacional e os cinco scripts de operação. Aceita a versão
Debian como primeiro argumento e gera o checksum em `dist/`.

#### `test-server-package.sh`

Inspeciona um `.deb` do servidor sem instalá-lo. Valida metadados, versão da
imagem, configuração Compose, links, documentação, ausência de segredos e a
sintaxe dos scripts empacotados. Sem argumento, seleciona o pacote mais recente
em `dist/`.

#### `build-all-debian-packages.sh`

É o orquestrador da entrega conjunta. Incrementa a versão piloto, compila o
cliente, gera os dois pacotes e valida ambos. Chama os cinco scripts de build e
teste descritos acima; não duplica internamente suas implementações.

### Publicação

#### `publish-client-release.sh`

Compila os binários portáteis, gera e valida o `.deb`, envia o pacote e seu
SHA-256 para o diretório de downloads e publica por último o manifesto
`agent-release.json`. A troca atômica do manifesto impede que a interface
aponte para um envio parcial. Uma versão existente só pode ser republicada se
o conteúdo tiver exatamente o mesmo checksum.

Destino, diretório e origem pública podem ficar no arquivo local ignorado
`.private/deploy/client-release.env`. Use como base
`deploy/admin-ui/release.env.example`. Com esse arquivo configurado, o fluxo é:

```bash
make publish-client-release
```

O script não remove versões anteriores. O pacote e o manifesto ficam
disponíveis em `/downloads/`; a `admin-ui` consulta o manifesto em tempo de
execução, portanto uma nova versão do agente não exige recompilar o painel.

#### `publish-admin-ui.sh`

Executa `npm run build` em `admin-ui/`, valida o acesso SSH e envia o conteúdo
de `admin-ui/dist/` para um host já preparado. Destino, diretório remoto e URL
pública da API podem ser mantidos no arquivo local ignorado
`.private/deploy/admin-ui.env`. Com esse arquivo configurado uma vez, o fluxo
rotineiro é somente:

```bash
make publish-admin-ui
```

Opções de linha de comando e as variáveis
`COMPASSO_ADMIN_UI_DEPLOY_TARGET`, `COMPASSO_ADMIN_UI_DEPLOY_DIRECTORY` e
`COMPASSO_API_BASE_URL` continuam disponíveis para substituir a configuração
local. Consulte `publish-admin-ui.sh --help`. A publicação usa `scp` e
substitui arquivos de mesmo nome sem remover artefatos antigos do diretório
remoto.

#### `publish-server.sh`

Orquestra uma publicação do servidor. Valida parâmetros e estado do Git,
executa testes, gera e inspeciona o pacote, verifica o host remoto, envia o
artefato, instala ou atualiza e confirma o healthcheck externo. Pode operar
somente localmente com `--build-only`.

O último endereço SSH validado é salvo localmente em
`.compasso-publish-server-host` e aparece como padrão na execução seguinte. O
arquivo é ignorado pelo Git e não contém senha, usuário ou chave privada.

Esse script produz alterações externas e pede confirmações antes das etapas
relevantes. `--yes` aceita apenas confirmações classificadas como não críticas.

### Operação do servidor instalado

Os cinco scripts abaixo são copiados para `/opt/compasso-server/scripts/` pelo
`build-server-package.sh`.

#### `install-server.sh`

Realiza a primeira inicialização. Confere privilégios, oferece a instalação do
Docker no Debian quando necessário, cria configuração e diretórios
persistentes, constrói os contêineres e aguarda o healthcheck interno.

#### `update-server.sh`

Atualiza uma instalação existente. Exige privilégios, cria um backup, valida o
Compose, reconstrói e reinicia os serviços e termina chamando
`status-server.sh`.

#### `status-server.sh`

Mostra o estado dos serviços no Compose e consulta o healthcheck interno da
API. É usado diretamente pelo operador e ao final de `update-server.sh`.

#### `backup-server.sh`

Interrompe temporariamente a API quando ela está ativa, arquiva o diretório de
dados e reinicia o serviço por meio de um `trap`. O backup é validado e gravado
com permissão `0600`. É chamado automaticamente por `update-server.sh`.

#### `restore-server-backup.sh`

Restaura explicitamente um arquivo criado pelo backup. Restringe o arquivo ao
diretório configurado, valida seu conteúdo, exige a confirmação textual
`RESTAURAR` e preserva os dados anteriores para recuperação. Não é chamado
automaticamente por nenhum outro script.

### Verificações do repositório

#### `check-doc-links.sh`

Confere se os links relativos dos arquivos Markdown públicos apontam para
arquivos ou diretórios existentes. Materiais locais de `.private/` não entram
nessa validação.

#### `test-migrations.sh`

Aplica, em bancos SQLite temporários, todas as migrações do agente e do servidor
na ordem dos arquivos. Confere quantidade registrada, integridade e chaves
estrangeiras. Complementa os testes Go ao validar diretamente a sequência SQL.

#### `test-backup-restore.sh`

Cria um banco temporário com todas as migrações do servidor e uma família de
controle, gera o mesmo arquivo `server/` usado pela operação, altera o original
e restaura o backup. Confere checksum, conteúdo, versões, integridade e chaves
estrangeiras sem acessar Docker ou os diretórios reais do servidor.

#### `test-security-packaging.sh`

Verifica diretivas de hardening do systemd, sintaxe de scripts, configuração
segura do contêiner da API, isolamento da interface administrativa e exclusão
de configurações sensíveis do contexto Docker. Atua sobre as fontes; os testes
dos pacotes atuam sobre os `.deb` já montados.

## Dependências principais

- Go, Perl e ferramentas padrão de shell para testes, links e builds locais;
- Docker e Docker Compose para binários portáteis, validação do Compose e
  execução do servidor;
- `dpkg`, `dpkg-deb` e `dpkg --validate-version` para pacotes Debian;
- `sqlite3` para `test-migrations.sh` e `test-backup-restore.sh`;
- `ssh`, `scp` e `curl` somente para publicação remota;
- `appstreamcli` é opcional na validação do pacote do cliente.

Todos os scripts Bash usam `set -euo pipefail`. Execute-os pelos pontos de
entrada acima sempre que houver um fluxo consolidado; invoque scripts internos
diretamente apenas para diagnóstico ou validação de um artefato específico.
