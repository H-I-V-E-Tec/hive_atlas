# Ficha de sinal — schema

A **ficha** é a unidade de conhecimento do Atlas: um mecanismo de vulnerabilidade
reconhecível, program-agnostic, com evidência, condições e limites explícitos.

## Identidade e ciclo de vida

| Campo | Conteúdo |
|---|---|
| `id` | Identificador estável (ex.: `S-REDIR-01`, `C-02`). Não reusar id de ficha arquivada. |
| `version` | Versão da ficha. Muda a cada alteração de conteúdo promovida. |
| `state` | `candidata` → `revisada` → `ativa` → `arquivada`. |

- **candidata**: destilada, ainda não conferida.
- **revisada**: fonte e interpretação conferidas pelo operador (dojo).
- **ativa**: passou também pelo critério de avaliação definido para uso na caçada.
- **arquivada**: aposentada; mantida para histórico e regressão.

Só fichas `revisada`/`ativa` entram na biblioteca compartilhada (ver
[contrato-ingestao.md](contrato-ingestao.md)). Estados são propostas do sistema,
não campos consumidos pelo `hypotheses.py` atual.

## Campos de conteúdo

| Campo | Conteúdo |
|---|---|
| `origem` | URL, data, autoria quando disponível e trechos que sustentam a interpretação. **Sanitizada** (ver confidencialidade). |
| `mecanismo` | A suposição do sistema que falhou e a relação causal. |
| `sinais_observaveis` | O que poderia ser percebido **antes** de saber a conclusão do relato. É o gatilho de reconhecimento. |
| `pre_condicoes` | Comportamento, acesso, arquitetura e estado necessários para o mecanismo se aplicar. |
| `evidencia_esperada` | Tipo de artefato e observação que sustentam cada pré-condição. |
| `contraexemplos` | Casos parecidos que são legítimos ou não satisfazem o mecanismo. Primeira classe — reduzem falso positivo. |
| `confirmacao` | Evidência que distingue a hipótese de falso positivo (pergunta/teste sugerido). |
| `aplicabilidade` | Classes de fluxo, componentes e fronteiras de confiança relevantes. |
| `encadeamento` | Capacidades exigidas e produzidas; relação comprovável entre os elos (`feeds`). |
| `exemplos` | Casos positivos, negativos e incompletos com respostas revisadas. |
| `historico` | Fontes adicionais, alterações, avaliações e decisões de promoção. |

## Matchers (o que o motor avalia)

A diferença central para o `hypotheses.py` atual: além do gatilho textual, a
ficha declara **condições verificáveis** que o motor checa contra a evidência.

| Campo | Conteúdo |
|---|---|
| `patterns` | Trechos/regex que disparam o reconhecimento inicial do sinal. |
| `conditions` | Lista de condições estruturadas; cada uma resolve para `presente` / `ausente` / `desconhecida` sobre a evidência da sessão. |
| `feeds` | Ids de composições (`C-*`) que este sinal alimenta. |

Uma condição não deve ser só um segundo regex: representa uma pré-condição do
mecanismo (ex.: "o parâmetro reflete host externo", "o endpoint exige sessão
autenticada"). Quando a evidência não permite decidir, a condição é
`desconhecida` — nunca silenciosamente `presente`.

## Regra de confidencialidade (inegociável)

Porque a biblioteca é **compartilhada**: o sinal é **mecanismo, não o programa**.

- Fichas são program-agnostic por construção.
- `origem` guarda proveniência, mas **nunca** vaza especificidade de programa
  privado (host, endpoint, id, nome de cliente).
- Sinal derivado de write-up privado só entra na biblioteca em forma de mecanismo
  genérico.

É o análogo do controle de `classification` do hive_mind. A promoção pelo dojo é
a fronteira de confiança onde essa sanitização é verificada.

## Preservar a diferença fonte × hipótese

Um write-up pode **enriquecer** um sinal existente; não criar entrada nova só
porque a fonte é nova. Distinguir mecanismo confirmado na fonte de hipótese
candidata no programa atual.
