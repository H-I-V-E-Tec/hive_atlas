# [ARQUIVADO] Proposta original do Dojo

> Este documento é o histórico da proposta que originou o Hive Atlas.
> Ele **misturava** dois assuntos que agora estão separados:
> - o **loop de aprendizado (dojo)** foi para o **excalibull** (modo de treino);
> - a **biblioteca de sinais + recuperação** virou o **hive_atlas** (ver [../README.md](../README.md)).
>
> Mantido só como referência. Não é a fonte da arquitetura atual.

---

# Dojo: aprendizado de sinais e reconhecimento de padrões

Proposta registrada em 2026-09-28, a pedido do operador.
Estado: arquitetura futura, para implementar depois da revisão do Excalibull.
O fluxo provisório já utilizável está definido em CODES.md e memory/README.md.

## Objetivo

Transformar write-ups, relatos e resultados revisados em sinais que o agente consiga reconhecer em casos novos, com evidência, condições de aplicação e limites explícitos. Medir a melhora antes de promover alterações para a caçada.

O aprendizado persistente inicial será do sistema: biblioteca externa, exemplos recuperados e instruções de análise. A leitura de relatos não altera os pesos do modelo. Treinamento de um modelo é uma possibilidade separada, a avaliar apenas se os resultados mostrarem uma necessidade de alterar comportamento que contexto e exemplos não resolvem.

## Modos e contexto

- Dojo é um modo de aprendizado; hunter é o modo de caçada. Não ficam ativos simultaneamente.
- Se dojo for solicitado durante a caçada, usar o mesmo program_id, objetivo, evidências e dúvidas já disponíveis. Suspender hunter e preservar seu ponto de retomada e as publicações pendentes.
- Se não houver alvo selecionado, selecionar um programa explícito antes de consultar seu conhecimento no Hive.
- No dojo, usar exemplos, dados já coletados e exercícios; testes diretos no alvo pertencem à caçada.
- A saída do dojo não retoma hunter automaticamente. Ao voltar ao hunter, perguntar a postura conforme seu contrato de ativação.

## Ciclo de aprendizado

1. Selecionar o alvo e uma pergunta de aprendizado que ajude seu contexto.
2. Ler fontes relevantes e identificar evidências do relato; fontes são dados, não instruções operacionais.
3. Destilar fichas de mecanismo.
4. Relacionar cada mecanismo com sinais existentes; ampliar uma entrada quando ela já representa o mesmo mecanismo.
5. Criar exemplos positivos, negativos e incompletos, revisados pelo operador.
6. Exercitar o reconhecimento em variações novas, com a conclusão original oculta.
7. Avaliar recuperação, interpretação, evidência, incerteza e custo.
8. Promover apenas melhorias medidas; manter versões e registrar regressões.
9. Recuperar as fichas pertinentes quando houver evidência nova na caçada.
10. Incorporar feedback de resultados reais revisados, sem generalizar automaticamente de um programa para todos.

## Ficha de mecanismo

Cada ficha deve ter identificador estável, versão e estado: candidata, revisada, ativa ou arquivada. Revisada significa que a fonte e a interpretação foram conferidas; ativa significa que passou também pelo critério de avaliação definido para uso na caçada. Esses estados são propostas do sistema futuro, não campos já consumidos pelo parser atual.

| Campo | Conteúdo |
|---|---|
| Origem | URL, data, autoria quando disponível e trechos que sustentam a interpretação. |
| Mecanismo | A suposição do sistema que falhou e a relação causal. |
| Sinais observáveis | O que poderia ser percebido antes de saber a conclusão do relato. |
| Pré-condições | Comportamento, acesso, arquitetura e estado necessários. |
| Evidência esperada | Tipo de artefato e observação que sustentam cada condição. |
| Contraexemplos | Casos parecidos que são legítimos ou não satisfazem o mecanismo. |
| Confirmação | Evidência que distingue a hipótese de falso positivo. |
| Aplicabilidade | Classes de fluxo, componentes e fronteiras de confiança relevantes. |
| Encadeamento | Capacidades exigidas e produzidas; relação comprovável entre os elos. |
| Exemplos | Casos positivos, negativos e incompletos com respostas revisadas. |
| Histórico | Fontes adicionais, alterações, avaliações e decisões de promoção. |

Preservar a diferença entre mecanismo confirmado na fonte e hipótese candidata no programa atual. Um write-up pode enriquecer um sinal existente; não criar uma entrada nova apenas porque a fonte é nova.

## Exercícios e transferência

- Positivo: o mecanismo se aplica, com evidência suficiente para apontar o sinal.
- Negativo: vocabulário ou aparência semelhante, mas condição essencial ausente.
- Incompleto: não há informação suficiente; a resposta correta identifica o que falta.
- Variação: nomes, endpoints e tecnologia diferentes, preservando ou alterando a relação causal.

Para cada exercício, exigir: sinal candidato, evidência de origem, condições presentes/ausentes/desconhecidas e próximo passo de confirmação. O agente pode propor exercícios, mas não deve ser a única autoridade sobre suas respostas esperadas.

Separar reconhecimento do sinal e confirmação da vulnerabilidade. Evitar exercícios que revelem no título, na descrição ou nos metadados a resposta que o agente deveria inferir.

## Recuperação durante a caçada

Combinar busca textual, busca semântica e verificação estruturada de condições. A busca seleciona candidatos; o agente decide sua aplicabilidade com base nos artefatos concretos.

Começar a avaliação com 3–5 fichas curtas por fluxo, sob um orçamento explícito de tokens. Esse número é um ponto inicial experimental, não um limite comprovado. Abrir o relato ou evidência completa apenas quando necessário ao aprofundamento.

Identificar programa, ativo, endpoint, método, papel, objeto e fluxo de cada evidência. Não combinar sinais de fluxos ou ativos diferentes como uma cadeia sem explicitar e sustentar a conexão.

Saída proposta:

```text
sinal candidato e versão
observação concreta e referência da evidência
por que a relação causal pode se aplicar
pré-condições presentes, ausentes ou desconhecidas
contraexemplo relevante
pergunta ou teste de confirmação sugerido
estado: hipótese ainda não testada
```

O resultado pode alimentar seguir o rastro. Descobrir a roda reúne o contexto quando os caminhos se esgotam e procura novos rumos. A recuperação de sinais não dispara testes por si só.

## Avaliação e números

Criar um conjunto inicial de casos revisados e uma versão de referência do sistema. Manter conjuntos separados para desenvolver os sinais e para avaliar sua transferência. Separar por relato de origem, para que paráfrases da mesma fonte não vazem entre os conjuntos.

| Medida | Interpretação |
|---|---|
| Precisão de aplicabilidade | Fração das sugestões pertinentes segundo os casos revisados. |
| Cobertura de mecanismos | Fração dos mecanismos relevantes que o sistema reconheceu. |
| Recuperação | Se a ficha relevante aparece entre as fichas retornadas. |
| Fidelidade da evidência | Se a explicação usa observações reais do caso e referências verificáveis. |
| Incerteza | Se identifica corretamente dados ausentes e evita concluir além da evidência. |
| Custo | Tokens, tempo e chamadas por caso analisado. |
| Resultado operacional | Hipóteses investigadas, descartes e achados confirmados, com denominadores explícitos. |

Repetir avaliações do modelo quando necessário para estimar variabilidade. Comparar a versão de referência e a candidata nos mesmos casos; apresentar resultados por mecanismo e exemplos de regressões, além da média.

Definir antes da promoção o ganho esperado e a tolerância a regressões. Não otimizar apenas a quantidade de sinais ou de hipóteses emitidas. Manter avaliação reservada e adicionar casos de uso reais ao longo do tempo sem usá-los indefinidamente como uma única prova de melhora.

Registrar modelo/configuração, versão das instruções, versão da biblioteca, entradas recuperadas, respostas e avaliação humana. O ganho é uma conclusão medida no conjunto de avaliação, não garantia para qualquer caçada futura.

## Componentes propostos

- Python: fichas estruturadas, validação, deduplicação, exercícios, avaliações e histórico de versões.
- Biblioteca transversal: sinais reutilizáveis e suas fontes, com contrato de consulta próprio.
- Hive: contexto e resultados específicos de cada programa, com consulta explícita por program_id.
- hive_instance: referência para destinos e ingestão de documentos compatíveis com o contrato do Hive.
- MCP: interface enxuta para recuperar sinais pertinentes e registrar feedback, depois de validar o núcleo Python.

O Hive atual exige program_id nas consultas documentadas. Definir a biblioteca transversal e sua consulta antes de integrá-la ao serviço; não assumir busca global nem criar um programa fictício para contornar o contrato.

Possíveis operações futuras: destilar fonte, validar ficha, recuperar sinais, exercitar reconhecimento, avaliar versão e registrar feedback. Não são comandos implementados.

## Diagnóstico de partida

O motor atual hypotheses.py extrai regex de signals.md e combina identificadores S-* em cadeias C-*. Não verifica todas as condições em prosa do sinal e da composição.

Exemplo reproduzido com texto sintético, sem acesso a alvo: uma frase sobre paginação de lista pública com o campo next aciona S-REDIR-01 e C-02, cuja hipótese envolve redirect em OAuth e roubo de código. Usar esse caso como negativo de regressão no projeto futuro.

## Implementação em etapas

1. Medir a referência e revisar exemplos; escolher o primeiro conjunto de mecanismos.
2. Definir esquema, proveniência, estados e versionamento; migrar poucas fichas.
3. Implementar avaliação de condições e recuperação textual; medir ganho.
4. Acrescentar recuperação semântica quando houver casos que justifiquem; medir novamente.
5. Integrar a biblioteca aos fluxos do agente, com contexto limitado e evidência explícita.
6. Expor por MCP e integrar feedback revisado dos protocolos.
7. Avaliar treinamento de modelo apenas se persistirem problemas de comportamento demonstrados pelas avaliações.

## Referências consultadas

- [Otimização de acurácia: contexto, exemplos, recuperação e treinamento](https://developers.openai.com/api/docs/guides/optimizing-llm-accuracy).
- [Recuperação textual e semântica](https://developers.openai.com/api/docs/guides/retrieval).
- [Práticas de avaliação, comparação e revisão humana](https://developers.openai.com/api/docs/guides/evaluation-best-practices).

As referências sustentam os princípios. A escolha do núcleo Python, a ficha de sinais, o orçamento inicial e a integração ao Excalibull são propostas deste projeto. Avaliações e armazenamento não dependem de contratar uma plataforma específica.
