# Histórico de mudanças

Este projeto segue [Versionamento Semântico](docs/versioning.md). Como ainda não
há uma versão pública estável, as mudanças relevantes permanecem em
**Não lançado**.

## Não lançado

### Adicionado

- agente Linux offline-first com persistência SQLite;
- API de administração e sincronização;
- painel administrativo React com atualização por SSE;
- interfaces GTK para configuração e bônus local;
- empacotamento Debian separado para cliente e servidor;
- documentação pública, mapa do repositório e diretrizes de contribuição.

### Alterado

- projeto relicenciado sob a GNU Affero General Public License v3.0 ou
  posterior (`AGPL-3.0-or-later`), com avisos de componentes de terceiros nos
  artefatos distribuídos.
- a contabilização do agente agora é interrompida quando a sessão gráfica local
  deixa o estado `active`; bloquear a tela não altera a contagem.

### Segurança

- sessões administrativas com CSRF e cookies configuráveis;
- tokens individuais por dispositivo;
- serviço systemd e contêiner da API com hardening validado por testes.
