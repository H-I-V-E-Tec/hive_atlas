# Deploy e licenciamento

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
