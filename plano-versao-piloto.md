# Plano da versão piloto: hive_atlas

Data: 2026-10-02. Reúne as alterações no Atlas decididas na revisão de tecnologias do ecossistema e no desenho do `hive_burp` (`../hive_burp/docs/decisions/`).

## Resumo

| # | Alteração | Motivo | Tamanho |
| --- | --- | --- | --- |
| 1 | Negociação correta da versão do protocolo MCP | O servidor responde sempre `2024-11-05`, seja qual for a versão pedida | Pequeno |
| 2 | Fonte de evidência do `hive_burp` no watcher | O hive_burp entrega eventos v1 em JSONL por programa | Pequeno |
| 3 | Sanitização obrigatória no contrato de evidência v1 | O invariante "evidência sem PII nem credenciais" não está no contrato | Pequeno |
| 4 | Avaliação do modelo de embeddings junto com o hive_mind | O Atlas usa o mesmo modelo; a troca precisa ser coordenada | Médio |
| 5 | Biblioteca servida pela API em vez de Qdrant direto | Mesma direção do hive_mind: cliente sem token do Qdrant | Grande, depende da API |

## 1. Protocolo MCP

O `atlas/mcp_server.py` é um servidor MCP feito à mão, só com a biblioteca padrão. Diferente do hive_mind, ele não usa `outputSchema` nem `structuredContent`, então `2024-11-05` é tecnicamente correto. O problema é a negociação: ele ignora a versão que o cliente pede.

- **Piloto:** no `initialize`, responder com a versão pedida pelo cliente quando ela estiver numa lista suportada (`2025-11-25`, `2025-06-18`, `2025-03-26`, `2024-11-05`), senão com a mais recente suportada. Responder `ping`. Teste que cobre as duas situações.
- **Depois do piloto:** decidir entre o SDK oficial em Python (`mcp`) ou portar o servidor para Go com o SDK oficial, como o hive_mind e o hive_burp. O SDK em Python quebra a regra atual de "sem dependências". Portar para Go só vale se o Atlas for distribuído no mesmo pacote `hive`. A decisão fica para quando o instalador único (spec 003 do api-hive-center) for definido.

## 2. Evidência vinda do hive_burp

O hive_burp grava um JSONL por programa e sessão em `$XDG_DATA_HOME/hive_burp/<program_id>/<sessão>/events.jsonl`, com eventos v1 completos. O `SessionWatcher` e o `LineAdapter` já leem JSON por linha.

- Aceitar esse caminho como fonte do watcher, por configuração (`--source` ou variável de ambiente), sem acoplar o Atlas ao hive_burp.
- O `LineAdapter` não deve preencher `program_id` do contexto quando a linha já traz um diferente: evento de outro programa é descartado e contado, para nunca misturar programas num board.
- Respeitar a retenção do hive_burp: o Atlas não copia o JSONL; só lê.
- Teste com um JSONL sintético no formato da seção 6 do `../hive_burp/PLANO_IMPLEMENTACAO.md`.

## 3. Sanitização no contrato v1

O documento de arquitetura do ecossistema afirma que a evidência enviada ao Atlas não contém PII nem credenciais, mas `docs/contrato-evidencia.md` não exige isso. Hoje só o hive_burp sanitiza; o adaptador que lê o WAL do excalibull não tem essa regra.

- Acrescentar ao contrato: **o produtor sanitiza antes de emitir** (tokens, cookies, chaves de API, PII). O Atlas não persiste evidência e não a envia para fora da máquina.
- Defesa adicional no Atlas: um filtro leve no `LineAdapter` que recusa linhas com padrões evidentes de segredo (`Authorization: Bearer`, `Cookie:`, chaves com formato conhecido) e registra só a contagem.

## 4. Modelo de embeddings

O Atlas usa `nomic-embed-text`, o mesmo do hive_mind (`atlas/embed.py`). As fichas e as notas da HIVE são em português e inglês, e esse modelo foi treinado sobretudo em inglês. A avaliação foi feita e está em `../hive_mind/plano-versao-piloto.md`, seção 2: a recomendação é `qwen3-embedding:0.6b` (MRR 0,979 contra 0,418 do modelo atual, num corpus multilíngue parafraseado).

- Rodar `python3 -m atlas.eval` com cada candidato e incluir as métricas na comparação.
- Se o hive_mind trocar de modelo, recriar `atlas_signals_v01` com a nova dimensão (`atlas/store.py` cria a collection pela dimensão) e repovoar com `python3 -m atlas.push`. Com o acervo atual, pequeno, o custo é baixo.
- Gravar o nome do modelo no payload de cada ficha para impedir mistura de vetores.

## 5. Biblioteca pela API

Hoje o MCP do Atlas lê a collection `atlas_signals_v01` direto do Qdrant, com token de leitura no ambiente (`docs/deploy.md`). Isso repete o acoplamento que o hive_mind está retirando.

- Quando o serviço Mind em Go existir (ver `../api-hive-center/plano-versao-piloto.md`), expor `atlas_signals_search` pela mesma API, autorizado pela concessão `atlas` do membro.
- O MCP local passa a chamar a API; sem sessão, continua com a biblioteca local `signals/*.json` e busca léxica, como já faz hoje quando falta `QDRANT_API_KEY`.
- Fora do piloto: só depois da spec 005 (Mind leitura via HTTPS).

## Verificação

- `python3 -m pytest tests/ -q` continua verde.
- Testes novos: negociação de versão, `ping`, JSONL do hive_burp com evento de outro programa descartado, linha com segredo recusada.
