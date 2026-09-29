# Contrato de evidência — sessão → Atlas

Como a evidência de uma sessão de caçada chega ao Atlas para ele observar ao vivo.
Contrato **fino e versionado**, para não acoplar o Atlas às entranhas do
excalibull (lição de version-skew do hive_scout).

`schema_version: 1`

## Evento de evidência

A sessão emite eventos normalizados. O Atlas nunca lê o alvo; ele só consome
esses eventos, que descrevem evidência **já coletada** dentro de escopo
autorizado.

```json
{
  "schema_version": 1,
  "event_id": "uuid",
  "ts": "2026-09-29T14:00:00Z",
  "program_id": "acme",
  "asset": "api.acme.com",
  "kind": "endpoint | param | header | js_finding | nota",
  "flow": "authz | oauth | upload | ...",
  "endpoint": "/v1/users/{id}",
  "method": "GET",
  "role": "user | admin | anon",
  "object": "user:123",
  "value": "trecho concreto observado",
  "ref": "brain/wal#42 | targets/acme/recon.txt:10"
}
```

- `kind` classifica o artefato. `value` carrega o trecho concreto; `ref` aponta
  para a origem verificável (WAL do brain, arquivo em targets).
- `program_id/asset/endpoint/method/role/object/flow` são o que permite ao motor
  **não** combinar sinais de fluxos ou ativos diferentes como cadeia sem
  sustentar a conexão.
- Campos ausentes ficam nulos; o motor os trata como `desconhecido`, nunca
  presume.

## Origem dos eventos (adaptador)

O excalibull já escreve WAL em `brain/` e recon em `targets/`. O **adaptador de
origem** do Atlas lê isso e emite os eventos acima. Trocar a origem depois
(ex.: assinar ingestões do hive_mind por `program_id`) não muda o motor — muda
só o adaptador.

## Modos de entrega

- **Snapshot (batch)**: um lote de eventos de uma vez. Usado em teste e avaliação.
- **Ao vivo (watcher)**: o adaptador emite eventos conforme novos artefatos
  aparecem; o motor reavalia e reescreve o board.

O mesmo motor consome os dois — "ao vivo" é só o watcher re-alimentando o núcleo.

## Fronteiras

- Toda evidência já está dentro de escopo autorizado quando chega ao Atlas; o
  Atlas **não** revalida escopo (isso é do gate do excalibull) e **não** autoriza
  tráfego.
- O Atlas não persiste evidência de programa como conhecimento transversal:
  evidência alimenta a recomendação da sessão, não a biblioteca. Só o dojo,
  com revisão humana e sanitização, promove sinais.
