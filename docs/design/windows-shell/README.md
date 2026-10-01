# Design extraído dos conceitos Windows

Os PNG originais em `docs/design/windows-shell/` são a referência visual
primária. As anotações de texto, geometria e cor abaixo foram extraídas por OCR
e amostragem de pixels; servem para conferir a implementação e localizar
diferenças junto com a comparação visual direta.

Nenhum valor aqui foi escolhido por conta própria: cores vêm da amostragem dos
pixels, textos e posições vêm do OCR.

## Cores

| Papel | Medido em | Valor |
| --- | --- | --- |
| Fundo da página | add-time, settings | `#F3F4F1` / `#F5F5F3` |
| Botão primário | add-time, settings | `#DD6D50` / `#E67459` |
| Chip neutro | settings | `#D9DCD8` |
| Superfície de campo | settings | `#F5F4F2` |
| Texto | ambos | preto |

O coral `#DD6D50` e `#E67459` é o mesmo acento que a `admin-ui` já declara como
`--coral: #e76f51`. A identidade de cor está consistente entre os conceitos, o
instalador e o painel. A variação entre as duas telas é pequena o bastante para
ser o mesmo coral sob compressão de imagem, e o instalador já usa `#E8654A`.

## Adicionar tempo — 1136x1385

Marca e cabeçalho, depois um cartão centralizado com largura de cerca de 780.

| y | Elemento | Texto | Tamanho |
| --- | --- | --- | --- |
| 114 | Marca | `Compasso` | 169x40 |
| 256 | Título | `Adicionar tempo` | 611x95 |
| 382 | Subtítulo | `Escolha o período e confirme com a senha do responsável.` | 888x37 |
| 482 | Chip de estado | `Servidor conectado` | 257x30 |
| 558–658 | Segmento de duração | `15 min` `30 min` `60 min` `120 min` | 4 itens de ~110x40 |
| 747 | Rótulo do campo | `Senha do responsável` | 331x38 |
| 837 | Campo de senha | `Digite sua senha` | 232x40 |
| 1002 | Botão primário | `Adicionar 30 minutos` | 350x39 |
| 1122 | Aviso | `A senha não é armazenada.` | 326x29 |
| 1242 | Link | `Configurações` | 212x37 |

Os quatro segmentos de duração estão alinhados na mesma linha, com folga regular,
em `x` = 168, 395, 630 e 853. O botão primário acompanha o texto do segmento
selecionado, que na imagem é 30 minutos.

## Configurações — 1114x1412

Janela com barra de título do sistema, os botões minimizar, maximizar e fechar no
canto superior direito em `x` = 1037, 961 e o indicador de janela.

| y | Elemento | Texto | Tamanho |
| --- | --- | --- | --- |
| 211 | Marca | `Compasso` | 203x48 |
| 305 | Título | `Configurações do Compasso` | 625x52 |
| 369 | Subtítulo | `Conecte este computador ao seu painel familiar.` | 524x30 |
| 432 | Chip de estado | `Ainda não configurado` | 239x27 |
| 498 | Título da seção | `Conta que será controlada` | 308x30 |
| 541 | Rótulo | `Conta Windows` | 158x27 |
| 591 | Valor da conta | `Sergio` | 76x34 |
| 675 | Aviso de consentimento | `Confirmo que esta conta poderá ter a sessão bloqueada pelo Compasso.` | 724x31 |
| 757 | Título da seção | `Conexão com o painel` | 259x30 |
| 800 | Rótulo | `Endereço do servidor` | 211x27 |
| 851 | Valor do campo | `https://apifamily.smresume.com` | 348x30 |
| 926 | Rótulo | `Identificador do dispositivo` | 266x26 |
| 977 | Valor do campo | `Cole o identificador gerado no painel` | 378x29 |
| 1051 | Rótulo | `Token do dispositivo` | 205x26 |
| 1103 | Ação do campo | `Mostrar` | 88x28 |
| 1163 | Aviso | `O token fica protegido neste computador.` | 391x28 |
| 1244 | Botão primário | `Salvar e conectar` | 205x30 |
| 1325 | Link | `Voltar para adicionar tempo` | 280x29 |

O token tem ação `Mostrar` no lado direito, o que indica campo de senha com
revelação. Os três campos de `Conexão com o painel` compartilham o mesmo
recuo, alinhando a coluna de valores.

## O que a extração não cobre

Tipografia, pesos, cantos arredondados, sombras, espaçamento fino e a posição
das formas decorativas continuam desconhecidos. A estrutura e o texto foram
recuperados; o acabamento não. Qualquer divergência nesses detalhes precisa
ser conferida contra o PNG por avaliação visual humana.

## Correções do usuário, 2026-09-29

A descrição textual do usuário prevalece sobre o que o OCR mediu, porque é mais
recente e porque o OCR não distingue glifo de rótulo.

| Ponto | Extraído | Confirmado pelo usuário |
| --- | --- | --- |
| Tempos | `15 30 60 120` | `15 30 60 120` |
| Aviso da senha | texto solto | ícone de informação, em itálico, "A senha **não será** armazenada." |
| Configurações | link no meio da pilha | no rodapé, com ícone de engrenagem |
| Seleção de tempo | não medida | repouso com fundo claro e texto escuro; somente a escolhida fica coral com texto branco |

Os períodos formam uma seleção exclusiva: escolher outro período desmarca o
anterior, mantém exatamente uma opção ativa e atualiza o texto do botão
principal para a nova duração.

Os campos de texto e senha das duas interfaces usam contorno uniforme nos
quatro lados, com cantos arredondados. A linha inferior do template Fluent
padrão não faz parte destes conceitos, nem no estado de foco.

O usuário chegou a citar "15, 10, 60 e 120" e depois confirmou `15, 30, 60, 120`.
Prevalece a confirmação.

O texto original do usuário continha erros de digitação e o termo "header" em
lugar de cabeçalho. Só o sentido foi usado.
