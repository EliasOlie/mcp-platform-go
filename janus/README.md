# Janus Core — Cérebro Operacional (Go Edition)

O **Janus Core** é a evolução em **Golang** do cérebro operacional do ecossistema, projetado para alta performance, consumo mínimo de memória (~20 MB de RAM) e busca contextual cirúrgica.

Ele é totalmente autônomo, sem dependências de serviços externos de autenticação legados, com suporte nativo a **RAG Híbrido** e interface **MCP (Model Context Protocol)**.

---

## 🚀 O que ele faz

1. **Decisões Literais Auditáveis (`decisions`)**:
   - Registros fiéis de decisões tomadas em canais e conversas, com autor, grupo, tema, timestamp e link original.
   - Nada é parafraseado: a resposta é sempre o registro histórico real.
2. **Fila de Ingestão de Candidatos (`decision_candidates`)**:
   - Extrações de conversas com pontuação de confiança antes de virar uma decisão oficial.
3. **Regras de Negócio (`rules`)**:
   - Regras operacionais derivadas e vinculadas a decisões.
4. **Base de Conhecimento Dinâmica (`knowledge_items`)**:
   - Multi-sistema (WinThor, regras financeiras, tributárias, especificações de DLL, FAQs, artigos).
   - Metadados de confiabilidade: `procedencia` (`verificado_query`, `confirmado_pessoa`, `hipotese`).
   - Validade temporal de dados: `snapshot_em` e `reverificar_apos`.
   - Vínculos relacionais com regras e outros itens.
5. **RAG Híbrido (pgvector HNSW + Full-Text Search + RRF)**:
   - **Busca Vetorial Densa**: Similaridade de cosseno com índice HNSW.
   - **Busca Léxica Esparsa**: PostgreSQL Full-Text Search com ranking `ts_rank`.
   - **Reciprocal Rank Fusion (RRF)**: Fusão dos dois rankings eliminando falsos positivos e pegando tanto conceitos quanto termos exatos de tabelas (`PCPRODUT`, CFOP, etc.).
6. **Interface Dupla (REST + MCP Server)**:
   - Endpoints REST para integrações web e microserviços.
   - Protocolo MCP JSON-RPC 2.0 nativo em `POST /mcp` para agentes (Antigravity, Cursor, Claude Desktop).

---

## 🛠️ Stack Tecnológica

- **Linguagem:** Go (Golang 1.23+)
- **Framework Web:** [Fiber v2](https://github.com/gofiber/fiber) (mesma stack do VPSQL)
- **Banco de Dados:** PostgreSQL 16 + extensão `pgvector`
- **Driver DB:** `pgx/v5` com suporte a `pgvector-go`
- **Embeddings:** Cloudflare Workers AI (`@cf/baai/bge-m3`, 1024 dimensões) / VPSQL AI Gateway

---

## 📦 Como Rodar

```bash
# 1. Configurar variáveis de ambiente
cp .env.example .env

# 2. Compilar
make build

# 3. Rodar
make run
```

O servidor iniciará em `http://localhost:3030`.

### Health Check
```bash
curl http://localhost:3030/health
```

---

## 🔌 Ferramentas MCP Disponíveis (`POST /mcp`)

- `search_knowledge`: Busca híbrida semântica e léxica em decisões, regras e conhecimento.
- `save_knowledge`: Salva um item de conhecimento com auto-registro de domínio e trilha de auditoria.
- `list_domains`: Lista todos os domínios de conhecimento e totalizadores de itens ativos e substituídos.
