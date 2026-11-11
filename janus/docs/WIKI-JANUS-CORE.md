# Janus Core (Go Edition) — Guia de Arquitetura e Uso

O **Janus Core** é o cérebro operacional do ecossistema do Elias (@EliasOlie), desenvolvido em Golang para máxima performance, mínimo consumo de memória e busca contextual cirúrgica.

---

## 1. Princípios Fundamentais

1. **Respostas Literais**:
   - Decisões de negócio nunca são parafraseadas; o histórico original é preservado com autor, grupo e data.
2. **Evolução Não-Destrutiva**:
   - Itens e decisões alterados não são sobrescritos; cria-se uma nova versão enquanto a anterior ganha status `substituido` com ponteiro para o sucessor (`substituido_por_id`).
3. **Autonomia Total**:
   - Sem dependência de serviços externos legados de autenticação corporativa.

---

## 2. RAG Híbrido com RRF (Reciprocal Rank Fusion)

A busca combina duas fontes complementares:
1. **Vetorial Densa (pgvector)**: Similaridade de cosseno com índice HNSW para capturar o sentido conceitual da pergunta.
2. **Léxica Esparsa (PostgreSQL FTS)**: `tsvector` com dicionário português e índice GIN para recuperar termos técnicos exatos, tabelas de banco (`PCPRODUT`), códigos de erro e parâmetros.
3. **Fusão RRF**: Algoritmo `1 / (60 + rank)` unifica os dois rankings eliminando falsos positivos e falsos negativos.

---

## 3. Gestão de Wikis de Clientes e Projetos (`tipo: "artigo"`)

- Ao documentar um cliente ou projeto, use `tipo: "artigo"` e informe o slug em `sistema` (ex: `cliente_alpha`, `gestao_pessoas`).
- Wikis longas indexam apenas `titulo + descricao` no vetor semântico para não diluir o espaço vetorial.
- O campo `relacionados_ids` conecta a Wiki narrativa aos itens atômicos (queries SQL, regras, endpoints) que ela costura.

---

## 4. Ferramentas MCP Nativas (Stdio e HTTP)

Disponibiliza 12 ferramentas padronizadas para Antigravity, Claude Code e Cursor:
- `search_knowledge`
- `save_knowledge`
- `get_knowledge`
- `get_wiki`
- `list_knowledge`
- `list_knowledge_domains`
- `record_decision`
- `supersede_decision`
- `supersede_knowledge`
- `delete_knowledge`
- `merge_domains`
- `get_audit_log`
