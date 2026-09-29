# Deploy e licenciamento

## Subir a biblioteca no servidor do hive_mind (lab local)

O Atlas reusa a MESMA caixa do hive_mind (Qdrant + Ollama + túnel/JWT), mas com
uma **collection própria transversal** (`atlas_signals_v01`, sem `program_id`).
O MCP continua **local**; só os dados ficam no servidor.

```bash
# 1. sobe Qdrant + Ollama (compose do hive_mind) e puxa o modelo
cd ../hive_mind && docker compose up -d qdrant ollama
docker compose exec ollama ollama pull nomic-embed-text

# 2. cria a collection do Atlas e emite tokens (usa a admin key do hive_mind)
cd ../hive_atlas
HIVE_QDRANT_ADMIN_KEY=... scripts/provision_qdrant.sh
# → imprime os export para popular (token rw) e o token r para o MCP

# 3. popula a biblioteca (rw)
export QDRANT_URL=http://127.0.0.1:6333 ATLAS_COLLECTION=atlas_signals_v01
export OLLAMA_URL=http://127.0.0.1:11434 EMBEDDING_MODEL=nomic-embed-text
export QDRANT_API_KEY=<token rw>
python3 -m atlas.push

# 4. registra o MCP apontando para o Qdrant (o token vem do ambiente, não do arquivo)
bin/hive install atlas --local \
  --qdrant-url http://127.0.0.1:6333 --ollama-url http://127.0.0.1:11434 \
  --collection atlas_signals_v01 --model nomic-embed-text
# depois, no shell que sobe o cliente:  export QDRANT_API_KEY=<token r>
```

Detecção de backend (`atlas/mcp_server.py`): com `QDRANT_API_KEY` no ambiente, o
MCP carrega a biblioteca do Qdrant + recuperação semântica; sem ele, cai no
`signals/*.json` local + recuperação léxica (mantém dev e testes offline). Falha
de conexão ao Qdrant também cai para o local, com aviso.

Migração para o **remoto** (atlasgrid.site): trocar `QDRANT_URL` por `https://…`
com CA/túnel válidos e usar um token individual, como o §12 do
`guias/CONFIGURAR_QDRANT_E_CODEX.md`. Nunca publicar 6333/6334 em `0.0.0.0`.

---

# Deploy do MCP e licenciamento

## `hive install atlas`

Objetivo: um launcher `hive` único (gerenciador de pacotes da Hive) que instala
qualquer componente em um comando, colapsando o fluxo atual do hive_instance
(clonar repo + credenciais à mão + `./hive install`).

```bash
bin/hive install atlas --local                 # modo dev: registra o MCP deste checkout
bin/hive install atlas --local --client ~/.../.mcp.json
bin/hive install atlas --local --require-license
```

- `--local` registra o MCP a partir do checkout (pula download de release).
- Sem `--local`, o caminho de release assinada é **fail-closed** enquanto
  `atlas-release.json` tiver `version: UNSET` — igual ao inicializador do
  hive_instance. O download/verificação de release assinada é o TODO(release).
- O comando auto-registra a entrada do MCP no `.mcp.json` do cliente
  (Claude Code / Codex), sem edição manual.

Manifesto: [`atlas-release.json`](../atlas-release.json) (esquema espelhando
`hive_instance/hive-release.json`: `repository`, `version`, `require_signature`).

### Decisão em aberto

O launcher unificado é **evolução do hive_instance** (que já tem
`install/doctor/validate/update`) ou um **bootstrap novo e mínimo**? Vale para
toda a Hive; o Atlas é só o primeiro consumidor limpo. O `bin/hive` aqui é a
prova de conceito do subcomando `install atlas`.

## Licenciamento

Verificação **offline** no startup do MCP (`atlas/license.py`), fail-closed no
modo comercial.

| Env | Efeito |
|---|---|
| `ATLAS_REQUIRE_LICENSE=1` | Exige licença válida; sem ela o MCP recusa iniciar (exit 2). |
| `ATLAS_LICENSE=<token>` | O token de licença. |
| `ATLAS_LICENSE_KEY=<hex>` | Chave de verificação. |

Modo interno/equipe (sem `ATLAS_REQUIRE_LICENSE`): acesso pela credencial de
leitor já usada na Hive.

**Nota de segurança:** a implementação atual usa HMAC-SHA256 (segredo simétrico)
para ficar em stdlib puro. Em produção migrar para **assinatura assimétrica**
(Ed25519 via `cryptography`), para o cliente verificar sem poder emitir licença.
A interface `issue`/`verify` não muda.

Ponto a desenhar cedo: **o que a licença destrava** (assentos, programas, acesso
à biblioteca compartilhada hospedada) — define onde a biblioteca vive e como o
MCP autentica.
