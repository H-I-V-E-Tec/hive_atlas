# Deploy e licenciamento

## Deploy pela release assinada (produção)

Fluxo automatizado, espelhando o do `hive_mind`: publica uma release assinada e
pede ao servidor que a baixe, verifique e publique a biblioteca no Qdrant. O
Atlas reusa o **mesmo** Qdrant/Ollama do `hive_mind`; o deploy só garante a
collection transversal `atlas_signals_v01` e roda `atlas.push`. Não sobe serviço
nem toca o Mind.

### Preparação única (servidor)

- Configure o environment `production` no GitHub com os secrets `DEPLOY_HOST`,
  `DEPLOY_PORT`, `DEPLOY_USER`, `DEPLOY_SSH_KEY` e `DEPLOY_KNOWN_HOSTS`. O usuário
  SSH precisa poder executar, via `sudo` sem senha, somente
  `/usr/local/sbin/hive-atlas-pull-release`.
- Instale [`deploy/pull_server_release.sh`](../deploy/pull_server_release.sh) como
  `/usr/local/sbin/hive-atlas-pull-release` (root:root, `0755`). Ele não é
  atualizado automaticamente; recopie-o quando mudar.
- O servidor precisa de `curl`, `cosign`, `jq`, `sha256sum`, `tar` e `python3`
  (3.10+), além do Qdrant/Ollama do `hive_mind` já no ar.
- As credenciais vêm do `hive_mind`: `/srv/hive-private/admin.key` (a admin key,
  usada como `api-key`) e `/srv/hive-private/ca.crt` (a CA do Qdrant TLS). Para
  repositório privado, um token GitHub com acesso a **H-I-V-E-Tec/hive_atlas** e
  permissão **Contents: read** em
  `/srv/hive-private/github-release.token` (root:root `0600`).
  O mesmo arquivo usado pelo `api-hive-center` pode ser reaproveitado, desde
  que o token também tenha acesso ao Atlas. O `GITHUB_TOKEN` do workflow não
  é transmitido ao servidor.
- Overrides opcionais em `/srv/hive-private/atlas.env` (root:root `0600`), a
  partir de [`deploy/atlas.env.example`](../deploy/atlas.env.example).

O bootstrap consulta a release pela API do GitHub e baixa cada asset pelo ID
com `Accept: application/octet-stream`, como o `api-hive-center`. A URL
`github.com/.../releases/download/...` pode retornar `404` para releases privadas
mesmo com um token válido. Depois de atualizar este script, recopie-o para
`/usr/local/sbin/hive-atlas-pull-release` no servidor; ele não é atualizado pelo
workflow. Um `404` na **consulta da API** indica tag inexistente ou token
ausente, inválido ou sem acesso ao repositório.

O fluxo de download e verificação tem teste próprio, sem rede nem deploy real:
`bash deploy/test_pull_server_release.sh`. Ele cobre API autenticada, token
ausente, metadados e assets inválidos e falhas de assinatura/checksum.

### Publicar e implantar

1. Na aba **Actions**, rode **Start release** a partir de `master` com uma tag
   nova `vX.Y.Z`. Ela cria a tag e dispara **Build and Release**, que testa,
   monta o bundle `atlas-server-vX.Y.Z.tar.gz`, assina o `SHA256SUMS` com a
   identidade OIDC do GitHub e publica tudo na GitHub Release.
   A mesma release inclui `atlas-vX.Y.Z.pyz`, o cliente MCP portátil instalado
   nas máquinas dos usuários pelo launcher `hive`.
2. Rode **Deploy Atlas library** com a mesma tag. O job exige que o bundle, o
   `SHA256SUMS` e o bundle de assinatura estejam publicados, conecta por SSH e
   chama `hive-atlas-pull-release`.
3. O servidor baixa, **verifica assinatura e checksum**, extrai e roda
   `deploy_server.sh`: sonda a dimensão do modelo, cria a collection se faltar,
   popula com `atlas.push` e confirma que a collection ficou com pontos antes de
   promover a release em `/opt/hive-atlas/current`.

Sem acesso ao servidor e aos secrets, dá para validar workflows, empacotamento e
scripts localmente (`bash deploy/test_deploy_server.sh`), mas a primeira
implantação real precisa de verificação operacional própria.

A emissão de um token `rw` com escopo só da collection do Atlas (em vez de usar a
admin key diretamente no push) fica como refinamento de menor privilégio — a
lógica já existe em [`scripts/provision_qdrant.sh`](../scripts/provision_qdrant.sh).

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

O launcher de produção é o [`hive_cli`](../../hive_cli/README.md). Ele instala o
Atlas da mesma origem fixa e pelo mesmo caminho de verificação Sigstore do
Mind. A release do Atlas publica um **zipapp** `atlas-vX.Y.Z.pyz`: fonte Python,
biblioteca local e identidade (`version`, SHA completo de `revision`) em um
arquivo portátil. Requer **Python 3.10 ou superior** no PATH (`python3` ou
`python`; no Windows também `py -3`). Não usa `pip` nem extrai o pacote.

Depois de publicar as alterações nos dois repositórios e gerar releases novas:

Como o repositório do Atlas é privado, a instalação pelo launcher exige
`GH_TOKEN` (ou `GITHUB_TOKEN`) no ambiente, com **Contents: read** e acesso ao
`H-I-V-E-Tec/hive_atlas`. Com esse token, o launcher também usa a API de assets.
O token do arquivo do servidor não é transmitido à máquina do usuário. Depois
de instalar, `hive atlas` não precisa da credencial GitHub.

```bash
hive update                            # atualiza o launcher e os produtos instalados
hive install atlas                     # última release com o cliente .pyz
hive install atlas --version vX.Y.Z    # ou uma versão específica
hive atlas version --json
hive version
hive version --json
hive atlas                             # MCP stdio; espera mensagens do agente
```

O launcher valida a identidade OIDC fixa do workflow da tag, a assinatura do
`SHA256SUMS` e o SHA-256 do `.pyz`, depois roda `version --json` e exige a mesma
tag solicitada. Uma falha mantém a versão anterior ativa. O comando de versão
não precisa de licença nem de Qdrant/Ollama; o início do MCP continua aplicando
o gate de licença. Releases antigas contendo só o bundle do servidor não podem
ser instaladas como cliente.

```bash
hive update atlas --check
hive update atlas
hive rollback atlas
hive uninstall atlas
```

`hive version` mostra o Atlas ao lado do Mind e do launcher, inclusive a versão
anterior e a atualização conhecida. `hive atlas` inicia o pacote ativo com o
Python em modo isolado; as credenciais e configurações `QDRANT_*`, `OLLAMA_*`,
`EMBEDDING_MODEL` e `ATLAS_*` continuam vindo do ambiente.

### Registrar no agente

Configure o comando do MCP como o caminho absoluto retornado por `command -v hive`,
com argumentos `["atlas"]`. O caminho do launcher fica estável após updates.
O `hive setup` atual registra o Mind; o Atlas deve ser cadastrado separadamente.

Claude (`.mcp.json`):

```json
{
  "mcpServers": {
    "hive_atlas": {
      "command": "/caminho/absoluto/.hive/bin/hive",
      "args": ["atlas"]
    }
  }
}
```

Codex (`config.toml`, mesclar com a configuração existente):

```toml
[mcp_servers.hive_atlas]
command = "/caminho/absoluto/.hive/bin/hive"
args = ["atlas"]
```

Sem credencial do Qdrant, o MCP usa a biblioteca local incluída no pacote.
No cliente instalado, o feedback persiste em `~/.hive/atlas/feedback.jsonl`
(ou `$HIVE_HOME/atlas/feedback.jsonl`). `ATLAS_FEEDBACK_FILE` permite definir
outro destino; o feedback não fica dentro da release e sobrevive aos updates.

### Desenvolvimento no checkout

O `bin/hive` deste repositório é a prova de conceito local. Ele registra o MCP
diretamente deste checkout; a distribuição de produção é responsabilidade do
launcher `hive_cli`.

```bash
bin/hive install atlas --local                 # modo dev: registra o MCP deste checkout
bin/hive install atlas --local --client ~/.../.mcp.json
bin/hive install atlas --local --require-license
```

- `--local` registra o MCP a partir do checkout (pula download de release).
- Sem `--local`, este script recusa a instalação; use o launcher de produção.
- O comando mescla a entrada do MCP no `.mcp.json` do Claude Code.

[`atlas-release.json`](../atlas-release.json) continua sendo o manifesto da
prova de conceito local. Ele não determina a origem ou a versão no launcher de
produção, que usa o registro fixo em `hive_cli/internal/registry`.

Para montar e testar o cliente localmente, sem publicar nem assinar:

```bash
python3 scripts/build_client_release.py --version v0.0.0 --revision "$(git rev-parse HEAD)"
python3 -I dist/atlas-v0.0.0.pyz version --json
python3 -m pytest -q tests/test_client_release.py
```

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
