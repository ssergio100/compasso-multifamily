---
name: design
description: Use quando a tarefa envolver criar, alterar ou avaliar qualquer componente, tela, layout, cor ou interação da admin-ui (front-end em admin-ui/src). Também aciona para propostas de UX/visual do painel de família. Ajuda a propor um design que preserve a harmonia, a simetria e a coerência entre telas antes de escrever código.
---

# Skill de design — admin-ui

A admin-ui é um painel de controle familiar. As decisões de visual e interação
devem preservar a harmonia, os padrões de interação e a coerência entre telas.
Antes de propor qualquer mudança visual, siga este procedimento.

## Procedimento obrigatório

1. **Proponha antes de implementar.** Descreva a solução em texto: onde entra,
   como Interage, o que muda em desktop e celular. Dê 1 recomendação e,
   quando houver dúvida real, 1 ou 2 alternativas.
2. **Confira a coerência entre telas** (Agenda/Editar rotina, Comunicação,
   Configurações, cadastros, Agora). Nada novo deve "descolar" do restante.
3. **Não quebre simetria.** Grades com contagem par (2, 4, 6) devem continuar
   pares. Não acrescente um bloco avulso a uma grade regular; se o número de
   itens crescer, mantenha a contagem par ou mova o novo item para fora da grade.
4. **Prefira escolhas restritas a entrada livre.** Usuários domésticos querem
   decisões rápidas: presets, steppers e seletores em vez de campos de texto
   livre (números) sempre que possível. Evite campos numéricos aberto.
5. **Pense no celular primeiro.** A família usa o painel no celular. Verifique
   o comportamento nas quebras de 960px e 600px e em telas estreitas.
6. **Acessibilidade e toque.** Alvos de toque com altura mínima (44–84px),
   foco visível (`--focus`), e rótulos legíveis. Espaços só com hover não
   existem no celular.
7. **Reuse o que já existe.** Se há um componente pronto (stepper
   `.time-range-control`, presets `.preset-grid`, modais `.modal`, pickers,
   `.switch`, alerta `.routine-conflict-alert`), use-o. Não crie variações
   visuais paralelas.

## Identidade visual

- **Fonte serifada** `Newsreader` (títulos grandes e números hero, peso 400,
  `letter-spacing` negativo).
- **Fonte UI** `Manrope` (todo o resto; strong 650 para destaques).
- **Fundo geral** `--fog:#f7f7f4`; **superfícies brancas** `#fff` e `#fafafa`;
  **bordas** `--line:#d8d5d7`.
- **Tinta** `--ink:#261d2b` (preto suave, botão primário e texto).
- **Destaque** `--coral:#e76f51` (laranja, tempo restante/atividades).
- **Esverdeado** `--sage:#819484`, `--sage-soft:#e9ede8` (positivo/sucesso).
- **Rotina/bloqueio** `--routine:#927aa0`, `--routine-dark:#6d5778`.
- **Estados**: online `#397a55`, danger `--danger:#a34545`,
  warning `--warning:#9b681f`, `--warning-soft:#f7eedf`, muted `--muted:#716b73`.
- **Sombras/Janelas**: `--shadow:0 18px 55px rgb(38 29 43 / .1)`; cantos
  arredondados ~10–18px (avatar 9–15px, céu do sheet 18px).

## Padrões de interação

- **Ativo = ink.** Item selecionado fica com fundo `--ink` e texto branco
  (tiles, tabs, botões primários). Hover de contêiner: `#f1f0f0`.
- **Botões**: altura mínima 48–84px; ação destrutiva usa `.danger-confirm`;
  primário usa `.primary` com ícone + rótulo (ex.: `+ Mais tempo`).
- **Modal**: largura `min(520px,100%)`, título serifado 2.1rem, subtítulo muted
  `.82rem`, ações em `.modal-actions` (2 colunas); no celular `<600px` vira
  bottom sheet com topo arredondado e altura `96dvh`.
- **Seleção múltipla**: switch/cards; escolha única: pills/tabs com `active`.
- **Conteúdo flexível**: `min-width:0` + `ellipsis` quando o texto pode vazar.
- **Roda de tempo** (.time-ring): `conic-gradient` com `--ink` (já usado) e
  `--coral` (restante), consumo do topo no sentido horário; número central com
  `Newsreader`; usado em cadastro de limite (diâmetro maior) e no herói "Agora".

## Responsividade

- Desktop 3 colunas (`280px 210px 1fr`); >960px revela número grande do herói
  (`.time-hero-number`) e esconde a roda móvel (`.time-balance`).
- `max-width:960px`: esconde rail/nav, mostra header móvel, navegação inferior;
  no herói mostra a roda consumida (`.balance-ring`) e esconde o número.
- `max-width:600px`: paddings 16px, `.preset-grid` vira 2 colunas, modal em
  sheet, alvos maiores (70px).

## Regras específicas

- **Mais tempo (modal bônus)**: presets `15/30/45/60` em `.preset-grid`.
  Limite do servidor: 1 min a 12h (`bonus must be between 1 minute and 12
  hours`). Se adicionar "Personalizado", mantenha a grade com contagem par e
  use controle restrito (stepper de `.time-range-control` ou presets extras),
  nunca campo numérico livre no meio do grid.
- **Agora (herói)**: estado `blocked`/`paused` zera a contagem e preserva o
  restante — o centro da roda mostra "tempo preservado"; offline mostra nota
  "saldo calculado com os últimos dados" acima da roda.
- **Alerta de conflito**: uso `.routine-conflict-alert` (ícone + título +
  detalhe) para erros dentro de modais.

## Idiomas

- Interface em PT-BR, imperativo claro e curto ("Mais tempo", "Bloquear",
  "Adicionar"). Labels de valor formatados como `1h 30min` (formato
  `formatDuration`, `admin-ui/src/features/common/format.ts`).

## Para avaliar uma proposta pronta

- Confira alinhamento com tokens e padrões acima.
- Verifique simetria de grades e restrição de entrada.
- Verifique comportamentos de estado (.state-blocked/paused/offline) e celular.
- Aponte impacto antes de implementar; se algo quebrar coerência, proponha
  alternativa antes de codar.