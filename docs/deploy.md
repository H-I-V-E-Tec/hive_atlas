# Deploy, cliente Go e autorização

## Desenho operacional

O serviço oferece MCP Streamable HTTP em `<center>/atlas/mcp`. O agente usa esse
endpoint diretamente ou a ponte stdio `hive atlas`, que encaminha mensagens MCP
com a sessão emitida pelo único `hive login`. Todas as ferramentas executam no
servidor. Sem sessão ou permissão, o modo normal falha; `--offline` seleciona
explicitamente o motor embutido para desenvolvimento.

Observe recebe evidência sanitizada por HTTPS, avalia sem persistir os eventos e
responde com o board `[UNTESTED]`. Feedback é persistido por membro no serviço,
fora da biblioteca de sinais. O watcher filtra os eventos localmente e chama
observe remoto; grava apenas o board na máquina do usuário.

## Preparação do servidor

- Qdrant/Ollama já disponíveis. O deploy do Mind precisa preceder o Center
  para aceitar a sessão compartilhada; não é necessário reiniciar os bancos.
- `curl`, `cosign`, `jq`, `sha256sum`, `tar`, `systemd` e `sed`. O runtime Atlas
  é um binário Go estático; o servidor não precisa instalar dependências Python.
- `/srv/hive-private/admin.key` e `/srv/hive-private/ca.crt` existentes. A admin
  key é usada somente pelo deploy/push e para emitir a chave restrita de leitura.
- `/srv/hive-private/atlas.env` (root:root, `0600`), conforme
  [atlas.env.example](../deploy/atlas.env.example). Confirme a interface gateway
  do Compose do Center: `ATLAS_HTTP_ADDR=172.28.0.1:8444` e o mesmo endereço em
  `ATLAS_UPSTREAM`. Para outra rede, ajuste ambos. O healthcheck usa essa interface.
- Instale [pull_server_release.sh](../deploy/pull_server_release.sh) como
  `/usr/local/sbin/hive-atlas-pull-release` (root:root, `0755`). O helper é
  administrado separadamente e precisa ser recopiado quando mudar.
- No GitHub, environment `production` com `DEPLOY_HOST`, `DEPLOY_PORT`,
  `DEPLOY_USER`, `DEPLOY_SSH_KEY`, `DEPLOY_KNOWN_HOSTS`. O usuário SSH só precisa
  de sudo sem senha para o helper root acima.

O repositório e as releases continuam públicos. O download consulta a API da
release e usa os IDs dos assets, sem token GitHub por padrão. O helper mantém
suporte opcional a `HIVE_ATLAS_GITHUB_TOKEN_FILE` para uma futura origem privada,
mas não lê automaticamente a credencial usada pelo Center.

## Preparação única para v2.0.0

O workflow chama um helper root já instalado; o bundle não atualiza esse helper.
Copie `deploy/pull_server_release.sh` desta branch para o servidor como
`/tmp/hive-atlas-pull-release.sh` e execute:

```bash
sudo install -o root -g root -m 0755 /tmp/hive-atlas-pull-release.sh \
  /usr/local/sbin/hive-atlas-pull-release
```

Confira `/srv/hive-private/atlas.env` (root:root, modo `0600`):
`HIVE_CENTER_URL=https://hive-center.duckdns.org`,
`ATLAS_HTTP_ADDR=172.28.0.1:8444` e
`ATLAS_HEALTH_URL=http://172.28.0.1:8444/healthz`, ou os valores da sua rede.
Não sobrescreva um arquivo existente com o exemplo. O serviço precisa da CA e
admin key já provisionadas em `/srv/hive-private`; o deploy emite automaticamente
a credencial de leitura. `ATLAS_UPSTREAM` em `/opt/hive-center/.env` precisa apontar
para a mesma interface; se o gateway padrão for o correto, o Compose aplica o
valor padrão sem precisar editar esse arquivo. Restrinja 8444 à rede interna.

O deploy instala o unit, cria `/var/lib/hive-atlas`, publica a biblioteca e
inicia o MCP. Não é preciso criar usuário, instalar Go/Python, emitir JWT manual
ou executar `push` no servidor. Não há nova migration de banco neste incremento;
o deploy do Center executa as migrations pendentes normalmente.

Depois dessa preparação, merge/publicação e deploy nas versões:
Mind `v1.5.0` → API Center `v1.3.0` → Atlas `v2.0.0`.
Publique também o launcher `v1.2.0`; no computador execute `hive update`,
`hive update mind atlas`, `hive login`, `hive setup atlas --client codex` e
`hive doctor atlas`. O usuário precisa de `product.atlas` no seu perfil do Center.
Se Atlas ainda não estiver instalado, use `hive install atlas`.
O frontend existente é reaproveitado pelo deploy da API; não há release web nova
necessária para a sessão compartilhada.

## Publicar e implantar

1. Atualize primeiro o serviço Mind para aceitar `aud=hive` e manter os tokens
   legados. Atualize o Center para emitir a sessão compartilhada, depois o
   launcher e o Atlas. Tokens legados `mind`/`atlas` continuam aceitos no respectivo
   serviço durante a transição; o novo login solicita somente `hive`.
2. Em `master`, execute **Start release** com uma tag nova (por exemplo `v2.0.0`).
   **Build and Release** executa a CI e publica os cinco clientes nativos,
   `atlas-server-vX.Y.Z.tar.gz`, `.pyz` de compatibilidade, `SOURCE.txt`,
   `SHA256SUMS` e os dois formatos de assinatura Sigstore.
3. Execute **Deploy Atlas library** com a mesma tag. O helper baixa os assets,
   valida a identidade OIDC do workflow da tag e o checksum do bundle.
4. O deploy confere versão/revisão do executável, publica uma collection imutável
   `<ATLAS_COLLECTION>_<tag>_<revision12>` e exige a contagem esperada de fichas.
   Cada payload registra modelo e dimensão; mistura de modelos é recusada.
5. Emite um JWT Qdrant `r` restrito à collection (validade de um ano, renovado a
   cada release), instala o unit e promove o symlink `current`.
   O healthcheck exige a nova versão, fichas presentes e busca semântica funcional.
   Falha de push mantém a versão anterior; falha do serviço restaura o symlink
   e o unit anteriores, com sua própria collection e credencial.

O unit usa `DynamicUser`, `LoadCredential`, filesystem protegido e nenhuma chave
admin. `StateDirectory=hive-atlas` mantém feedback em `/var/lib/hive-atlas`, com
modo `0700`, inclusive após updates/rollback; defina uma política de backup desse
estado junto à operação do serviço. As credenciais por release e a CA ficam em `deploy/` como root `0600`,
fora do Git/bundle público; o systemd as entrega ao serviço. A configuração
não secreta é `deploy/service.env`. A API fica na interface privada do Center,
atrás do HTTPS do proxy. `atlas serve` isolado usa `127.0.0.1:8444` por padrão.

Uma versão já ativa e saudável é idempotente. Alterações de configuração exigem
uma nova tag: não se reescreve a configuração da release ativa. Collections antigas
são preservadas para rollback; esta entrega não faz sua remoção automática.

## Proxy do Center

A mudança coordenada em `api-hive-center/deploy` adiciona:

```caddyfile
handle_path /atlas/* {
    reverse_proxy {$ATLAS_UPSTREAM}
}
```

`compose.yaml` passa `ATLAS_UPSTREAM` ao Caddy, com padrão `172.28.0.1:8444`.
O Center emite JWT RS256 com `aud=hive`, sujeito do membro, perfis, expiração e
permissões efetivas. O serviço valida JWKS/issuer/expiração/audiência e exige
`product.atlas` em toda chamada MCP e REST. Um JWT compartilhado com somente
`product.mind` é válido, mas recebe 403 no Atlas. O login de um usuário com acesso
somente ao Atlas também funciona. Não é necessário alterar o frontend.

O MCP valida Origin quando presente; a origem do Center é permitida por padrão.
`ATLAS_ALLOWED_ORIGINS` permite origens adicionais separadas por espaço.
POST retorna JSON; notificações aceitas retornam 202 sem corpo. O endpoint é
stateless, sem IDs de sessão, GET/SSE permanente ou persistência de evidência.
GET e DELETE autenticados retornam 405 conforme o transporte.

## Launcher e instalação

A versão atualizada do `hive_cli` prefere o asset Go listado no `SHA256SUMS`
assinado. Para releases anteriores contendo somente `.pyz`, mantém o runtime
Python. Um asset nativo listado, mas ausente/corrompido, gera erro; não cai para
Python. Dispatch e rollback identificam o formato efetivamente instalado.
A origem e a identidade Sigstore continuam fixas em `H-I-V-E-Tec/hive_atlas`.

```bash
hive update
hive install atlas
hive atlas version --json
hive login
hive setup atlas --client codex
hive doctor atlas
hive atlas
hive update atlas --check
hive update atlas
hive rollback atlas
```

`hive login` e `hive logout` pertencem ao launcher e independem de instalar Mind
ou Atlas. A sessão fica em `$HIVE_HOME/token` (padrão `~/.hive/token`) e a URL do
Center em `$HIVE_HOME/center-url`, ambos com acesso privado. `hive login --check`
valida assinatura e claims da sessão. `HIVE_TOKEN_FILE` e `HIVE_TOKEN` são opções
compartilhadas de injeção. `hive setup atlas` e `hive doctor atlas` são atalhos
para `hive atlas setup|doctor`. Não há token ou login específico do Atlas.

As permissões são um retrato da emissão do JWT. Refaça `hive login` após alterar
perfis. O prazo atual é de 12 horas; não foi acrescentado refresh/revogação
imediata aos tokens já emitidos.

`HIVE_CENTER_URL` ou `--center-url` define outro Center. A URL é lembrada após
login, e o cliente também reconhece a URL salva pelo Mind. `HIVE_ATLAS_URL` ou
`--atlas-url` substitui `<center>/atlas`. HTTP só é permitido em loopback;
redirects de requisições autenticadas são recusados.

## Registro manual no agente

`hive atlas setup --client codex` mescla a tabela abaixo no TOML; `--config`
permite um arquivo específico. Para Claude Code, `--client claude-code` usa
`~/.claude.json`; para Claude Desktop, informe `--client claude-desktop --config`.
O setup usa o caminho estável `HIVE_LAUNCHER` fornecido pelo launcher e registra
`HIVE_HOME`, `HIVE_CENTER_URL` e `HIVE_ATLAS_URL` no ambiente da ponte, sem copiar
o JWT. O exemplo mínimo abaixo usa as configurações padrão. Agentes HTTP
podem usar diretamente `<center>/atlas/mcp` e o JWT compartilhado no Bearer.

```toml
[mcp_servers.hive_atlas]
command = "/caminho/absoluto/.hive/bin/hive"
args = ["atlas"]
```

```json
{"mcpServers":{"hive_atlas":{"command":"/caminho/absoluto/.hive/bin/hive","args":["atlas"]}}}
```

## Verificação local

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

Os testes usam fixtures sintéticas: JWT/JWKS compartilhados compatíveis com o Center, MCP HTTP remoto,
Qdrant/Ollama falsos, verificação de push e rollback sem systemd real.
Eles verificam comportamento local; a implantação de uma nova release no servidor
precisa ser executada e conferida pelos workflows de produção.

## Referência Python e licença antiga

`atlas/`, `bin/hive` e `build_client_release.py` permanecem como referência,
ferramentas do dojo e compatibilidade de distribuição. O runtime principal é Go.
O gate opcional `ATLAS_REQUIRE_LICENSE=1` mantém a verificação HMAC da licença
antiga durante a migração; ele é independente da autorização RS256 da API.
Nenhuma chave HMAC é usada para autorizar acesso à biblioteca hospedada.
