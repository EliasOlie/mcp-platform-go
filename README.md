# mcp-platform-go

**Self-hosted platform for giving AI agents scoped, auditable access to company knowledge and tools — written in Go.**

Three small services (≈20 MB RAM each) that together answer one question: *how do you let Claude, Cursor or any MCP client use your internal systems without handing them the keys to everything?*

```
              [ Agents: Claude / Cursor / any MCP client ]
                                  │  HTTPS + Bearer eo_live_...
                                  ▼
                    ┌───────────────────────────┐
                    │          mcp-hub          │  :8082 /mcp
                    │  unified MCP gateway      │  RFC 9728 metadata
                    └──────┬─────────────┬──────┘
                 verify    │             │  proxy (only tools the
                 token     ▼             ▼  caller is allowed to see)
              ┌─────────────────┐  ┌──────────────────────────┐
              │  auth-service   │  │          janus           │
              │  :8081          │  │  :3030 (internal only)   │
              │  Ed25519 · JWKS │  │  hybrid RAG · pgvector   │
              └─────────────────┘  └─────────────┬────────────┘
                                                 ▼
                                     PostgreSQL 16 + pgvector
```

## Why it exists

This is the open, generalized version of an architecture I run in production for a retail & wholesale group in Brazil, where agents answer operational questions on top of a legacy ERP. The lessons that shaped it:

- **Identity is central, authorization is local.** The auth service only proves *who* is calling and returns a flat permission list (`janus:read`, `vpsql:query`). Each service decides what those permissions mean. No business rules leak into the identity layer.
- **Agents should never see tools they can't use.** `mcp-hub` builds a per-request MCP server containing only the tools the caller's scopes allow. An out-of-scope `tools/call` fails at the protocol level — there is no handler to trick with prompt injection.
- **Knowledge needs provenance, not just embeddings.** Every item in Janus carries where it came from (`procedencia`: verified by query, confirmed by a person, or hypothesis), when it was true (`snapshot_em`) and when to re-check it (`reverificar_apos`). Decisions are stored literally and superseded, never edited in place.

## Services

### `auth-service` (`:8081`)
- Multi-tenant identity provider for humans and machines (M2M).
- JWTs signed with **Ed25519**, public keys at `/.well-known/jwks.json` (RFC 7517).
- API keys `eo_live_<prefix>_<secret>`: only the SHA-256 is stored, compared in constant time, revocable.
- `POST /auth/verify` with rate limiting — the single call every other service makes.

### `janus` (`:3030`, internal network only)
- Knowledge base and decision log for agents.
- **Hybrid RAG**: pgvector HNSW (1024 dims) + Postgres full-text search, merged with **Reciprocal Rank Fusion** — catches both concepts and exact identifiers (table names, tax codes) that pure vector search misses.
- REST + MCP interfaces; results are always the literal record plus a score, never a paraphrase.

### `mcp-hub` (`:8082`)
- One `/mcp` endpoint for all internal tools, with OAuth Protected Resource Metadata (RFC 9728) so claude.ai Custom Connectors can discover the authorization server.
- Dynamic per-tenant tool gating, transparent proxy to Janus and other backends.

### `pkg/eosecurity`
- Reusable Fiber middleware that validates callers against `auth-service`.

## Permissions

| Permission | Grants |
| :--- | :--- |
| `janus:read` | `search_knowledge`, `get_knowledge`, `get_wiki`, `list_domains` |
| `janus:write` | `save_knowledge`, `record_decision`, `supersede_*`, `delete_*` |
| `janus:*` | every Janus tool |
| `vpsql:query` | controlled analytical queries through the VPSQL gateway |

## Running locally

Requires Go 1.25+ and Docker.

```bash
cp .env.example .env    # set DATABASE_URL, ADMIN_MASTER_KEY, BOOTSTRAP_ADMIN_*
make build              # ./bin/auth-server, ./bin/janus-server, ./bin/mcp-hub
make up                 # docker compose: auth :8081, janus :3030 (internal), hub :8082
```

Every default in the code is a placeholder — set real secrets through environment variables.

## Related

- [vpsql-control-plane](https://github.com/EliasOlie/vpsql-control-plane) — the Postgres control plane and AI gateway these services run on.
- [readonly-sql-mcp](https://github.com/EliasOlie/readonly-sql-mcp) — read-only SQL gateway pattern for legacy databases.

---

<sub>🇧🇷 Documentação interna e comentários em português; este README está em inglês para leitores internacionais.</sub>
