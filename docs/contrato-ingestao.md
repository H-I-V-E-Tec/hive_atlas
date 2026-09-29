# Contrato de ingestão — dojo → Atlas

Como o dojo (no excalibull) publica fichas na biblioteca do Atlas. Caminho de
**escrita**, separado do MCP de leitura (reader ≠ writer, como no hive_mind).
Contrato **versionado**: mudança de schema é mudança de versão, nunca implícita.

`schema_version: 1`

## Manifesto de ingestão

O dojo entrega um lote de fichas promovidas:

```json
{
  "schema_version": 1,
  "produced_by": "dojo",
  "produced_at": "2026-09-29T14:00:00Z",
  "signals": [ { /* ficha conforme ficha-schema.md */ } ]
}
```

## Portões de aceite (fail-closed)

A ingestão recusa o lote inteiro se qualquer ficha falhar:

1. **Estado válido**: só `revisada` ou `ativa` entram na biblioteca compartilhada.
   `candidata` é rejeitada — fica no dojo até ser revisada.
2. **Schema completo**: todos os campos obrigatórios de [ficha-schema.md](ficha-schema.md)
   presentes; `id` e `version` estáveis.
3. **Confidencialidade**: verificação de sanitização — nenhuma especificidade de
   programa (host, endpoint concreto, id, nome de cliente) fora de `origem`, e
   `origem` sanitizada. É a fronteira de confiança entre o dojo (pessoal) e a
   biblioteca (compartilhada).
4. **Enriquecer, não duplicar**: se a ficha já existe, ingestão incrementa
   `version` e registra em `historico`; não cria entrada nova só porque a fonte
   é nova.
5. **Regressão**: casos negativos conhecidos (ex.: paginação com campo `next` **não**
   deve disparar `S-REDIR-01`/`C-02`) continuam classificados como negativos.

## Versionamento e histórico

- Cada promoção registra em `historico`: fonte, alteração, avaliação e decisão.
- A biblioteca preserva versões; regressões são registradas, não sobrescritas.
- O ganho que justifica a promoção é uma **conclusão medida** no conjunto de
  avaliação do dojo, não garantia para qualquer caçada futura.

## O que a ingestão NÃO faz

- Não aprova sozinha: a revisão humana acontece no dojo, antes da ingestão.
- Não é ferramenta do MCP de leitura.
- Não ingere evidência de sessão (isso é o [contrato de evidência](contrato-evidencia.md),
  efêmero e não-transversal).
