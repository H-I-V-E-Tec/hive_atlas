# Hive Atlas 🗺️

> Biblioteca de **sinais** de segurança + motor que **observa a sessão ao vivo**
> e mostra o *melhor caminho a seguir*. Entregue como **servidor MCP**. Ativo
> **compartilhado** da Hive.
>
> Uso restrito a programas e ativos autorizados. O Atlas é **advisory**: nunca
> toca o alvo, nunca dispara teste e **nunca autoriza tráfego**. Toda saída é
> `[UNTESTED]`. O gate de escopo continua no excalibull.

## Onde o Atlas fica entre os projetos da Hive

| Projeto | Papel | Natureza |
|---|---|---|
| **hive_mind** | A mente da colmeia: leva informação de um membro a outro, mantém contexto entre sessões. Fonte canônica de scope/policy/recon/findings **por `program_id`** (Go + MCP + Qdrant). | Infra compartilhada |
| **excalibull** | Framework **pessoal**: regras, protocolos (`THE-MIND.md`, `CODES.md`), ferramentas e o modo *hunter*. Consome o hive_mind. **O `dojo` (treino) vive aqui.** | Pessoal |
| **hive_atlas** | **Este projeto.** Biblioteca de sinais (transversal, program-agnostic) + recomendação ao vivo, via MCP. | Compartilhada |
| **hive_scout** | Worker que mantém escopo fresco. | Infra compartilhada |

**Memória de método, não de fatos.** O hive_mind lembra *o que aconteceu em cada
programa*; o Atlas lembra *como reconhecer um mecanismo em qualquer programa*.
São camadas complementares.

## O ciclo: dojo produz, Atlas serve

```text
write-ups/findings revisados
        │  dojo (no excalibull) — usa o julgamento do operador como revisor
        ▼  destila · exercita · avalia · PROMOVE (fronteira de confiança)
biblioteca de sinais  ← hive_atlas (ativo compartilhado, contrato versionado)
        │  atlas observa a evidência da sessão ao vivo (contrato de evidência)
        ▼  emite "melhor caminho": sinais candidatos rankeados [UNTESTED]
hunter (excalibull)  → decide, confirma sob o gate de escopo, reporta
```

- **dojo** = produtor e QA dos sinais (vive no excalibull).
- **hive_atlas** = consumidor em runtime: biblioteca + recuperação + recomendação.

## O que o Atlas é e o que não é

- **É**: biblioteca de sinais reutilizáveis + motor de recuperação e recomendação,
  entregue como MCP. Raciocínio sobre **evidência já coletada** numa sessão
  autorizada.
- **Não é**: não toca o alvo, não dispara testes, não autoriza tráfego. Não é
  dono de fatos por programa (isso é hive_mind). Não é o loop de treino (isso é
  o dojo).

## Dois armazenamentos, dois contratos

O hive_mind não faz busca global e não aceita `program_id` fictício. Então o
Atlas tem **loja própria** para a biblioteca transversal e apenas **lê** a
evidência do programa ativo.

1. **Biblioteca de sinais (loja do Atlas)** — fichas reutilizáveis,
   program-agnostic. Conhecimento de *método*. Ver [docs/ficha-schema.md](docs/ficha-schema.md).
2. **Evidência da sessão (fonte externa)** — o que o Atlas observa ao vivo,
   sempre dentro de escopo autorizado. Ver [docs/contrato-evidencia.md](docs/contrato-evidencia.md).

A produção de fichas pelo dojo entra pelo [contrato de ingestão](docs/contrato-ingestao.md)
(caminho de escrita, separado do MCP de leitura).

## Interface MCP (proposta — validar o núcleo antes de crescer)

| Ferramenta | Ação |
|---|---|
| `atlas_observe` | Dado program/ativo + evidência da sessão, devolve o board de melhor caminho (sinais candidatos rankeados, `[UNTESTED]`). |
| `atlas_signals_search` | Recupera fichas candidatas (texto + semântica) sem avaliar a sessão inteira. |
| `atlas_feedback` | Registra desfecho revisado de uma recomendação. |

Reader ≠ writer: a ingestão de fichas **não** é uma ferramenta do MCP de leitura.

## Instalar o MCP — `hive install atlas`

O launcher [`hive_cli`](../hive_cli) instala o cliente portátil `atlas-vX.Y.Z.pyz`
da release do Atlas, verifica a assinatura Sigstore de `SHA256SUMS` e o checksum,
confere a versão do pacote e só então o ativa. Requer **Python 3.10+** no PATH;
não precisa clonar o repositório nem instalar dependências Python.

```bash
hive install atlas                     # última release assinada com cliente
hive atlas version --json              # identidade do pacote instalado
hive version                           # versões de hive, mind e atlas
hive atlas                             # inicia o MCP via stdio
```

O suporte ao Atlas precisa estar publicado no launcher e a release do Atlas
precisa conter o `.pyz`. Registro no agente, atualização e rollback estão em
[docs/deploy.md](docs/deploy.md). O startup continua exigindo licença válida
quando `ATLAS_REQUIRE_LICENSE=1`.

## Deploy da biblioteca no servidor

`Start release` → `Build and Release` (bundle assinado via Sigstore) →
`Deploy Atlas library` (SSH: o servidor baixa, verifica e roda `atlas.push`
contra o Qdrant do `hive_mind`). Passo a passo e preparação do servidor em
[docs/deploy.md](docs/deploy.md). O fluxo pode ser validado localmente com
`bash deploy/test_deploy_server.sh` (sem Qdrant/Ollama reais).

## Como rodar

Stdlib puro (Python 3.10+), sem dependências. `pytest` só para os testes.

```bash
python3 -m pytest tests/ -q          # suíte completa
python3 -m atlas.eval                # mede Atlas vs. hypotheses.py (baseline)
python3 -m atlas.mcp_server          # sobe o servidor MCP (stdio)
python3 -m atlas version --json      # checkout se identifica como dev
bin/hive install atlas --local       # registra o MCP no cliente (.mcp.json)
```

## Estado

Etapas 1–10 do plano implementadas em núcleo dependency-light e testadas
(29 testes):

| Módulo | Etapa | Papel |
|---|---|---|
| `docs/`, `README.md` | 1 | fundação e contratos |
| `atlas/ficha.py`, `evidence.py`, `engine.py` | 2 | núcleo: fichas + condições + board |
| `atlas/eval.py`, `eval/cases.json` | 3 | medição vs. `hypotheses.py` |
| `atlas/watcher.py` | 4 | observação ao vivo (tail → board) |
| `atlas/retrieval.py` | 5 | recuperação léxica + costura semântica |
| `atlas/mcp_server.py` | 6 | servidor MCP (stdio) |
| `atlas/ingest.py` | 7 | ingestão do dojo (fail-closed) |
| `bin/hive`, `atlas-release.json` | 8 | `hive install atlas` |
| `atlas/license.py` | 9 | licença offline (fail-closed) |
| `atlas/feedback.py` | 10 | store de desfechos |
| `atlas/embed.py`, `store.py`, `push.py`, `scripts/provision_qdrant.sh` | 11 | biblioteca hospedada no Qdrant + semântica |

**Subir no servidor do hive_mind (lab local):** ver [docs/deploy.md](docs/deploy.md)
— `provision_qdrant.sh` cria a collection transversal `atlas_signals_v01`,
`python3 -m atlas.push` popula, e o MCP usa Qdrant quando `QDRANT_API_KEY` está
no ambiente (senão, JSON/léxico local).

O que ainda requer validação operacional: releases assinadas e instalação real
via launcher, recuperação semântica com embeddings/Qdrant e licença assimétrica. O histórico
da proposta original está em [docs/historico-proposta-dojo.md](docs/historico-proposta-dojo.md);
deploy e licença em [docs/deploy.md](docs/deploy.md).
