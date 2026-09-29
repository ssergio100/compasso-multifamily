# Documentação do Compasso

Este índice reúne a documentação pública vigente. Registros históricos e notas
transitórias são preservados localmente, fora do pacote compartilhado.

## Para usuários e operadores

- [Demonstração e capturas de tela](demo.md)
- [Instalação do cliente Linux](client-installation.md)
- [Instalação da API do servidor](server-installation.md)
- [Atualização manual do servidor](atualizacao-manual-servidor.md)

## Para desenvolvimento

- [Ambiente e comandos de desenvolvimento](development.md)
- [Mapa do repositório](repository-map.md)
- [Geração de pacotes Debian](debian-packaging.md)
- [Versionamento](versioning.md)
- [Coleção Postman da API](Compasso_API.postman_collection.json)

As instruções específicas também estão nos READMEs de `agent/`, `server/`,
`admin-ui/`, `local-ui/`, `protocol/`, `packaging/`, `deploy/` e `scripts/`.

## Arquitetura e contratos

- [Arquitetura de comunicação](arquitetura-comunicacao.md): referência
  detalhada do contrato entre painel, servidor e agente;
- [Sincronização agente–servidor](synchronization.md): visão prática do saldo
  confirmado, âncoras e confirmações.

## Material local não publicado

Planos já executados, roteiros de pilotos, notas datadas, diagnósticos locais,
propostas ainda não aprovadas e materiais de design sem uso ficam em
`.private/`. Esse diretório é intencionalmente ignorado pelo Git e não faz parte
do pacote público. O `CHANGELOG.md` preserva o histórico relevante para quem
usa ou desenvolve o projeto.
