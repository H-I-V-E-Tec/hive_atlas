# Hive Atlas 🗺️

Release preparada: **v2.0.0**, branch `release/v2.0.0`. Builds de checkout
se identificam como `v2.0.0-dev`; a release recebe versão/revisão da tag assinada.

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

## Interface MCP

| Ferramenta | Ação |
|---|---|
| `atlas_observe` | Avalia evidência sanitizada no serviço remoto e devolve sinais rankeados `[UNTESTED]`. |
| `atlas_signals_search` | Busca fichas na biblioteca hospedada. |
| `atlas_feedback` | Registra um desfecho revisado no serviço, vinculado ao membro. |

A ingestão de fichas pelo dojo continua separada do MCP de leitura.

## Login unificado e MCP remoto

```text
hive login → Center → JWT RS256 (aud=hive, permissions=[...]) → ~/.hive/token
                          ├─ Mind: product.mind
                          └─ Atlas: product.atlas

Agente → hive atlas (ponte Go) → HTTPS /atlas/mcp → ferramentas no servidor
Agente com transporte HTTP → HTTPS /atlas/mcp → ferramentas no servidor
```

Um único `hive login` autentica o membro, mesmo sem produtos instalados. O Center
emite o JWT com suas permissões efetivas; Mind e Atlas leem a mesma sessão.
O Atlas verifica assinatura/JWKS, issuer, expiração e `product.atlas` em cada
chamada. Ausência da permissão resulta em 403; login ausente ou inválido gera erro.

O MCP remoto usa [Streamable HTTP](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)
em `<center>/atlas/mcp`. Observe, busca e feedback executam no servidor.
A ponte `hive atlas` adapta stdio para esse endpoint e relê o token compartilhado;
ela não baixa a biblioteca nem executa o motor no uso normal. Clientes HTTP podem
usar o endpoint diretamente com `Authorization: Bearer <JWT compartilhado>`.

Evidência sanitizada de observe atravessa HTTPS e é avaliada apenas durante a
requisição, sem ser gravada na biblioteca ou em logs. Feedback fica separado por
membro no estado persistente do serviço; não promove fichas automaticamente.
Qdrant/Ollama e suas credenciais pertencem somente ao serviço. `--offline` é um
modo explícito de desenvolvimento com a biblioteca embutida e feedback local.

## Instalar e usar

Com a release do launcher que incorpora o suporte nativo publicada:

```bash
hive update hive                       # atualize primeiro somente o launcher
hive install atlas
hive atlas version --json
hive login                            # sessão única para todos os produtos
hive setup atlas --client codex        # ou --client claude-code
hive doctor atlas
hive atlas                            # ponte para o MCP remoto
hive atlas --offline                  # desenvolvimento com biblioteca embutida
```

O launcher verifica assinatura Sigstore, checksum e versão antes de ativar.
Releases Go publicam binários para Linux/macOS (amd64/arm64) e Windows (amd64),
sem Python na máquina do usuário. Releases `.pyz` anteriores continuam
instaláveis e disponíveis para rollback. O `.pyz` também é publicado durante a
transição para launchers antigos; a integração com a API está no runtime Go.
As releases do Atlas permanecem **públicas**, sem exigir token GitHub.

Configuração: `HIVE_CENTER_URL` (padrão `https://hive-center.duckdns.org`),
`HIVE_ATLAS_URL` (padrão `<center>/atlas`), `HIVE_HOME` (padrão `~/.hive`),
`ATLAS_OFFLINE=1`. URLs fora de loopback precisam de HTTPS. O login guarda
`$HIVE_HOME/token`, compartilhado com o Mind; `HIVE_TOKEN_FILE` substitui o caminho
e `HIVE_TOKEN` permite injeção da sessão em clientes. `hive logout` remove a sessão
local compartilhada. No servidor, `ATLAS_STATE_DIR` define o estado persistente;
feedback fica em `feedback/<member_id>.jsonl`. No modo offline, o feedback vive em
`$HIVE_HOME/atlas/feedback.jsonl` (`ATLAS_FEEDBACK_FILE` permite outro destino). Configurações e operação: [docs/deploy.md](docs/deploy.md).

## Deploy

`Start release` → `Build and Release` → `Deploy Atlas library`.
O bundle assinado contém `hive-atlas` em Go. O servidor publica uma collection
por versão/revisão, emite uma credencial Qdrant de leitura restrita a ela e
inicia `hive-atlas serve` via systemd. O healthcheck valida a versão, a biblioteca
e a busca semântica. Falha restaura a release e a biblioteca anteriores.
O proxy do Center publica o MCP em `/atlas/mcp`.

## Desenvolver e verificar

Go conforme `go.mod`; sem CGO no Atlas. Python 3.10+ permanece como referência
de comportamento e ferramenta de build, não como dependência do cliente Go.

```bash
go test -race ./...
go vet ./...
go run ./cmd/hive-atlas eval
go build -o bin/hive-atlas ./cmd/hive-atlas
python3 scripts/check_go_parity.py --binary bin/hive-atlas
python3 -m pytest tests -q
bash deploy/test_deploy_server.sh
bash deploy/test_pull_server_release.sh
```

Watcher de evidência JSONL (por exemplo, produzido pelo hive_burp):

```bash
bin/hive-atlas watch --offline --program-id demo --asset api.demo.invalid \
  --source /caminho/da/sessao/events.jsonl --board /caminho/board.md
```

O watcher conta recusas de outro programa, credenciais evidentes e JSON inválido,
processa linhas completas e acompanha rotação/truncamento. O produtor sanitiza
credenciais e PII antes de emitir. Biblioteca e ingestão usam os contratos de
`docs/`; padrões Go são RE2 e padrões incompatíveis são recusados.

A avaliação atual tem 6 casos revisados e 3 fichas. A suíte compara o motor Go
com o Python no corpus, incluindo recuperação com orçamento; esse corpus ainda
precisa crescer para medir a qualidade em outros mecanismos e sessões.
A troca do modelo de embeddings continua pendente de avaliação coordenada.

O plano da entrega e as diferenças verificadas no Mind estão em
[docs/spec/go-remote.md](docs/spec/go-remote.md). O histórico da proposta
original está em [docs/historico-proposta-dojo.md](docs/historico-proposta-dojo.md).
