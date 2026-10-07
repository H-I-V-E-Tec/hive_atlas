# Atlas Go, login unificado e MCP remoto

## Contrato

- `hive login` pertence ao launcher e funciona sem produtos instalados.
- Center emite JWT RS256 `aud=hive` com membro/perfis/permissões efetivas atuais.
  Membros ativos podem autenticar mesmo sem `product.mind`; serviços exigem suas
  permissões. Suspensão impede emitir uma sessão nova.
- Mind e Atlas leem `$HIVE_HOME/token` e `center-url`; `HIVE_TOKEN` e
  `HIVE_TOKEN_FILE` são compartilhados. Não existe login/token próprio do Atlas.
- Cada serviço valida assinatura/JWKS, issuer e expiração; Mind exige
  `product.mind`, Atlas exige `product.atlas`. Tokens antigos do próprio produto
  continuam válidos até expirar, para permitir o rollout coordenado.
- `/atlas/mcp` é MCP Streamable HTTP stateless: initialize, ping, tools/list,
  tools/call, notificações 202; JSON na resposta, GET/DELETE 405, Origin validado,
  limites de corpo e negociação/header de protocolo.
- Observe, busca e feedback rodam no servidor. A ponte Go adapta stdio para o
  endpoint MCP e relê a sessão; não usa REST para avaliar o motor no cliente.
- Credenciais são recusadas antes do envio pela ponte e novamente no servidor.
  Evidência só existe durante a chamada; isolamento por programa/ativo/fluxo
  permanece. Feedback tem autoria derivada do JWT e arquivos separados por membro,
  fora da biblioteca, em estado privado persistente do systemd.
- Falha de autenticação não habilita motor local. `--offline` é exclusivamente
  uma escolha explícita de desenvolvimento/referência.

## Distribuição e deploy

Binários Go nas cinco plataformas, sem Python no cliente; assets públicos com
assinatura/checksum, identidade de origem fixa, rollback entre `.pyz` e Go.
Servidor recebe biblioteca revisada, Qdrant/Ollama privados, collection por release,
reader restrito, healthcheck semântico e rollback. `StateDirectory` preserva
feedback entre versões. O proxy do Center encaminha `/atlas/*` ao serviço.

Atualizar Mind primeiro (aceitação de `hive`), Center depois (emissão compartilhada),
seguido por launcher/Atlas. Tokens emitidos são retratos de permissões por até
12 horas; nova permissão exige novo login. Refresh/revogação imediata permanecem
fora deste incremento.

## Comparação verificada com o Mind

O `main` remoto do Mind foi conferido no commit `b4eb498`, igual ao checkout.
Ele já opera contra o serviço remoto em `<center>/mind`; setup inicia um
adaptador stdio que usa REST para executar as ferramentas remotamente. O Atlas
oferece adicionalmente um endpoint MCP HTTP real, permitindo agente HTTP direto
ou ponte stdio. Ambos usam a mesma identidade e sessão. Não foi alterado o
transporte existente do Mind; foi adicionada aceitação do JWT compartilhado.

## Aceite automatizado

- Emissão do Center para permissões Mind/Atlas/ambas/nenhuma, sem depender do Mind;
  negação de membros suspensos e preservação dos contratos legados.
- Login do launcher sem instalação; JWT verificado antes de persistir; falhas não
  substituem a sessão válida; logout e arquivo privado.
- Mesmo contrato `aud=hive` aceito no Mind e Atlas; serviços negam falta da sua
  permissão. O Mind apresenta ingestão apenas com `mind.ingest` e `product.mind`.
- MCP remoto initialize/observe/feedback com token compartilhado, sem biblioteca
  baixada, sem feedback local e sem credenciais atravessando a rede. HTTP nega
  expiração, assinatura/audiência/issuer inválidos, origem e versão de protocolo.
- Motor mantém o corpus Go/Python, e deploy usa fixtures de push/rollback.

Testes sintéticos não representam implantação/homologação em produção.
Referência: [MCP Streamable HTTP](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports).
