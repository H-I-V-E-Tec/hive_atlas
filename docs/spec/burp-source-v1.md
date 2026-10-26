# Fonte hive_burp v1 no watcher Atlas

Estado: implementação do consumidor Go; produtor e homologação ponta a ponta pendentes.
Contrato de origem: `hive_burp/docs/specs/009-eventos-atlas.md` e seus schemas
`atlas-source-v1` e `evidence-v1`. Esta spec implementa a parte do consumidor
dos critérios SPEC-009-AC01/02/03/04; não comprova SPEC-009-AC05.

## Requisitos

| ID | Requisito |
| --- | --- |
| ATLAS-BURP-R01 | `watch --manifest` lê somente manifesto v1 privado, regular e sem symlink, com programa configurado, estado `active`, relógio fresco (≤30 s) e prazo válido. A fonte é o `file_name` UUID no mesmo diretório privado. |
| ATLAS-BURP-R02 | Cada linha completa deve ser um envelope `evidence-v1` exato, com `event_id` UUID, `ref` vinculado a programa/sessão e campos limitados. Conteúdo inválido ou segredo é recusado sem aparecer no board/log. Os adaptadores legados Go/Python recusam eventos `hive_burp:` para impedir desvio do manifesto. |
| ATLAS-BURP-R03 | O watcher deduplica `event_id` em toda a geração, inclusive após sair da janela de 2.000 eventos. A leitura por poll é limitada; não interpreta linha parcial como nota. |
| ATLAS-BURP-R04 | Estado inativo, manifesto inválido/expirado, erro de fonte ou troca de identidade/revisão limpam corpus, offsets, IDs e board. Nova geração exige novo `file_name`. Antes de escrever o board, o watcher revalida o manifesto. O intervalo do modo manifesto é ≤10 s. |
| ATLAS-BURP-R05 | Projeção acima de 32 MiB falha fechada. Apenas o produtor pode reter/apagar a projeção; o consumidor apaga somente seu board. |

## Critérios de aceite

| ID | Prova |
| --- | --- |
| ATLAS-BURP-AC01 | Três kinds v1, ID repetido e linha parcial produzem só os eventos completos e únicos; schema/ref/segredo inválidos são recusados. Relaciona SPEC-009-AC01/02. |
| ATLAS-BURP-AC02 | Manifesto fechado, expirado, trocado ou ausente elimina corpus e board; programa diferente e fonte insegura não são lidos. Relaciona SPEC-009-AC03/04. |
| ATLAS-BURP-AC03 | Revalidação posterior à recomendação impede a gravação de board após invalidação concorrente. Relaciona SPEC-009-AC03. |

O modo JSONL legado permanece para fontes que não alegam ser do hive_burp.
O watcher não revalida o escopo externo nem autoriza captura: isso pertence ao
gate do produtor. Sem produtor, daemon, release assinado e teste Burp real, a
integração comercial continua desligada.
