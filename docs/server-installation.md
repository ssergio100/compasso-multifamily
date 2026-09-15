# Compasso Server — instalação no Debian 13

O pacote Debian instala o código e as ferramentas operacionais da API. A
interface administrativa é um build estático independente e não faz parte do
pacote. Nenhum dos dois configura túnel, proxy reverso, VPN, DNS, certificado
ou firewall.

## Instalação

Copie o `.deb` para o servidor e instale-o:

```bash
sudo apt install ./compasso-server_<versão>_all.deb
```

Revise `/etc/compasso-server/compasso.env` e execute:

```bash
sudo /opt/compasso-server/scripts/install-server.sh
```

O instalador da API:

1. verifica Docker Engine e Docker Compose e pede autorização antes de instalar
   uma dependência ausente;
2. cria os dados em `/srv/docker/volumes/compasso` e os backups em
   `/srv/docker/backups/compasso`;
3. constrói e inicia a API na porta configurada;
4. não solicita usuário, senha, domínio ou configuração de infraestrutura.

Por padrão, a API escuta em todas as interfaces do host na porta `8181`. O
painel deve ser compilado e servido separadamente conforme
[`deploy/README.md`](../deploy/README.md); o exemplo usa a porta `8182` e encontra
a API no mesmo host, porta `8181`.

O arquivo `.env` permite restringir o bind a `127.0.0.1`, trocar a porta,
escolher os diretórios de dados e backup e configurar a origem administrativa,
os cookies seguros e o envio de e-mail. HTTPS deve ser terminado pela
infraestrutura escolhida para a implantação.

Depois de implantar também o painel, abra `http://IP-DO-SERVIDOR:8182`. O painel
oferece cadastro autônomo com nome da família, e-mail e senha. A família só é
criada após a confirmação do e-mail; uma conta pendente expira em 24 horas.

Na primeira atualização de uma instalação antiga, entre uma última vez com o
acesso anterior. O painel abre **Minha conta** e pede um e-mail confirmado. O
cadastro de uma segunda família permanece fechado até essa confirmação; depois
dela, o login passa a ser o e-mail informado.

### E-mail de contas

Cadastro, recuperação e troca de e-mail exigem um servidor SMTP com STARTTLS.
Configure os quatro valores abaixo em `compasso.env`; usuário e senha podem
ficar vazios somente quando o relay não exige autenticação:

```dotenv
COMPASSO_SMTP_ADDRESS=smtp.exemplo.com:587
COMPASSO_SMTP_USERNAME=usuario
COMPASSO_SMTP_PASSWORD=segredo
COMPASSO_SMTP_FROM=contas@exemplo.com
```

`COMPASSO_TIME_ZONE` deve conter o fuso IANA usado pelos dispositivos atendidos,
por exemplo `America/Sao_Paulo`. A API usa esse fuso para apresentar o estado
atual das rotinas; os registros persistidos continuam em UTC.

Em uma implantação pública, `COMPASSO_ADMIN_ORIGIN` deve conter a origem HTTPS
exata do painel, por exemplo `https://compasso.exemplo.com`. Esse valor protege
as chamadas do navegador e também forma os links enviados por e-mail. O modo
`same-host` é adequado ao acesso local, mas, quando API e painel usam portas
diferentes, não deve ser usado para os links públicos.

Sem SMTP completo a API continua iniciando, mas os endpoints que precisam de
e-mail respondem `503`; portanto valide um cadastro e uma recuperação antes de
abrir o piloto. O servidor exige STARTTLS e TLS 1.2 ou superior e nunca registra
tokens de conta nos logs.

### Identidade dos agentes e métricas

Atualize primeiro todos os agentes existentes. Confirme que eles anunciam a
capacidade `installation-identity` e só então ative no servidor:

```dotenv
COMPASSO_REQUIRE_INSTALLATION_IDENTITY=true
```

Um agente antigo passa a receber `426 agent_upgrade_required`; uma segunda
instalação usando a mesma credencial recebe `409 installation_conflict`.
Rotacionar a credencial do dispositivo libera o vínculo anterior sem apagar a
política e o consumo centrais.

`GET /metrics` expõe contadores agregados de heartbeats aceitos, rejeitados,
limitados e com erro `5xx`, além do histograma de latência em formato
Prometheus. Restrinja esse endpoint à rede de monitoramento no proxy público.
Ele não contém IDs de família ou dispositivo.

### Diagnóstico de erros da API

Toda resposta JSON de erro contém `error`, um `code` estável e um
`correlation_id` exclusivo. Os mesmos valores são enviados nos cabeçalhos
`X-Compasso-Error-Code` e `X-Compasso-Correlation-ID`; o CORS os expõe somente
à origem administrativa autorizada. A interface traduz códigos conhecidos
para instruções em português e apresenta o código e a referência que devem ser
informados ao suporte.

Falhas `5xx`, origem administrativa recusada, CSRF inválido e respostas sem
código reconhecido são registradas com a mesma referência. O log não inclui
corpo, cookies, credenciais ou query string. Para localizar um incidente:

```bash
sudo docker compose logs compasso-api | grep 'correlation_id=REFERENCIA'
```

## Operação

```bash
sudo /opt/compasso-server/scripts/status-server.sh
sudo /opt/compasso-server/scripts/backup-server.sh
sudo /opt/compasso-server/scripts/update-server.sh
sudo /opt/compasso-server/scripts/restore-server-backup.sh /srv/docker/backups/compasso/compasso-server-DATA.tar.gz
```

Uma atualização preserva `.env` e o banco externo ao diretório do pacote. A
restauração exige confirmação textual e move os dados
anteriores para o diretório de backups antes de recuperar o arquivo escolhido.
O instalador ativa `compasso-server-backup.timer`, que executa um backup por dia
com atraso aleatório de até uma hora e recupera uma execução perdida durante
desligamento. Consulte `systemctl status compasso-server-backup.timer`; os
arquivos não são removidos automaticamente, portanto monitore o espaço e
preserve ao menos os sete dias mais recentes.

### Suspender ou reativar uma família

O operador pode usar o login do proprietário ou o ID interno da família. O
comando altera apenas o banco compartilhado e termina; ele não inicia outra
API. A suspensão invalida imediatamente as sessões existentes e a reativação
exige novo login.

```bash
cd /opt/compasso-server
sudo docker compose run --rm --no-deps compasso-api -suspend-family proprietario@exemplo.com
sudo docker compose run --rm --no-deps compasso-api -reactivate-family proprietario@exemplo.com
```

Executar novamente a ação já aplicada é seguro e informa `changed=false`.

Uma família ativa exclui a própria conta pelo painel. Em incidente ou abandono,
uma família suspensa só pode ser excluída pelo operador repetindo seu ID opaco
exato nos dois argumentos:

```bash
cd /opt/compasso-server
sudo docker compose run --rm --no-deps compasso-api \
  -delete-suspended-family ID-EXATO -confirm-family-id ID-EXATO
```

O comando recusa famílias ativas e confirma a remoção apenas no log local.

## Fronteira dos componentes

- `compasso-api`: Go, API JSON e SQLite; não contém nem serve HTML.
- `compasso-admin-ui`: Nginx não-root com HTML, CSS e JavaScript; não contém o
  servidor nem acessa o banco.
- agente cliente: não faz parte deste Compose e continua sendo um serviço
  systemd nativo na máquina monitorada.
