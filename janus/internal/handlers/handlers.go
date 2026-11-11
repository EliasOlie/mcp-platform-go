package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/eliasolie/janus/internal/config"
	"github.com/eliasolie/janus/internal/database"
	"github.com/eliasolie/janus/internal/models"
	"github.com/eliasolie/janus/internal/search"
	"github.com/eliasolie/janus/internal/service"
	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	cfg     *config.Config
	db      *database.DB
	service *service.Service
	search  *search.Engine
}

func New(cfg *config.Config, db *database.DB, svc *service.Service, engine *search.Engine) *Handler {
	return &Handler{
		cfg:     cfg,
		db:      db,
		service: svc,
		search:  engine,
	}
}

func (h *Handler) RegisterRoutes(app *fiber.App) {
	// Middleware opcional de autenticação via API Key
	if h.cfg.APIKey != "" {
		app.Use(func(c *fiber.Ctx) error {
			if c.Path() == "/health" {
				return c.Next()
			}
			auth := c.Get("Authorization")
			token := strings.TrimPrefix(auth, "Bearer ")
			if token != h.cfg.APIKey {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
			}
			return c.Next()
		})
	}

	app.Get("/health", h.HealthCheck)

	// API REST Completa
	api := app.Group("/v1")
	api.Get("/knowledge/search", h.SearchKnowledge)
	api.Get("/knowledge/item", h.GetKnowledgeItem)
	api.Get("/knowledge", h.ListKnowledge)
	api.Post("/knowledge", h.SaveKnowledge)
	api.Post("/knowledge/:id/supersede", h.SupersedeKnowledge)
	api.Delete("/knowledge/:id", h.DeleteKnowledge)
	api.Get("/knowledge/domains", h.ListDomains)
	api.Post("/knowledge/domains/merge", h.MergeDomains)
	api.Get("/audit", h.GetAuditLog)
	api.Post("/decisions", h.RecordDecision)
	api.Post("/decisions/:id/supersede", h.SupersedeDecision)
	api.Post("/candidates/ingest", h.IngestCandidate)

	// MCP Endpoint (JSON-RPC 2.0 Standard)
	app.Post("/mcp", h.HandleMCP)
	app.Get("/mcp", h.HandleMCPInfo)
}

func (h *Handler) HealthCheck(c *fiber.Ctx) error {
	ctx := c.Context()
	err := h.db.Pool.Ping(ctx)
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"ok":    false,
			"db":    "down",
			"error": err.Error(),
		})
	}
	return c.JSON(fiber.Map{
		"ok":      true,
		"service": "janus-core-go",
		"db":      "up",
	})
}

func (h *Handler) SearchKnowledge(c *fiber.Ctx) error {
	query := c.Query("query")
	if strings.TrimSpace(query) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "o parâmetro 'query' é obrigatório"})
	}

	filters := search.FilterOptions{
		Sistema:     c.Query("sistema"),
		Tipo:        c.Query("tipo"),
		Procedencia: c.Query("procedencia"),
		Status:      c.Query("status"),
	}

	hits, err := h.search.Search(c.Context(), query, filters)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"query":   query,
		"total":   len(hits),
		"results": hits,
	})
}

func (h *Handler) GetKnowledgeItem(c *fiber.Ctx) error {
	var input models.GetKnowledgeInput
	if idStr := c.Query("id"); idStr != "" {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			input.ID = &id
		}
	}
	if titulo := c.Query("titulo"); titulo != "" {
		input.Titulo = &titulo
	}
	input.IncluirHistorico = c.Query("incluir_historico") == "true"

	res, err := h.service.GetKnowledge(c.Context(), input)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"items": res})
}

func (h *Handler) ListKnowledge(c *fiber.Ctx) error {
	sistema := c.Query("sistema")
	tipo := c.Query("tipo")
	status := c.Query("status")
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))

	items, err := h.service.ListKnowledge(c.Context(), sistema, tipo, status, limit, offset)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"items": items})
}

func (h *Handler) SaveKnowledge(c *fiber.Ctx) error {
	var req models.SaveKnowledgeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "payload inválido"})
	}

	item, domain, err := h.service.SaveKnowledge(c.Context(), req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"item":   item,
		"domain": domain,
	})
}

func (h *Handler) SupersedeKnowledge(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}

	var req models.SaveKnowledgeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "payload inválido"})
	}

	novoItem, err := h.service.SupersedeKnowledge(c.Context(), id, req, req.Actor)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"message":   "Item substituído com sucesso",
		"novo_item": novoItem,
	})
}

func (h *Handler) DeleteKnowledge(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}

	if err := h.service.DeleteKnowledge(c.Context(), id, "api-user"); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"message": "Item removido com snapshot preservado na auditoria"})
}

func (h *Handler) ListDomains(c *fiber.Ctx) error {
	domains, err := h.service.ListDomains(c.Context())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"domains": domains})
}

func (h *Handler) MergeDomains(c *fiber.Ctx) error {
	var body struct {
		Origem  string `json:"origem"`
		Destino string `json:"destino"`
		Actor   string `json:"actor"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "payload inválido"})
	}

	res, err := h.service.MergeDomains(c.Context(), body.Origem, body.Destino, body.Actor)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

func (h *Handler) GetAuditLog(c *fiber.Ctx) error {
	entidade := c.Query("entidade")
	entidadeID := c.Query("entidade_id")
	limit, _ := strconv.Atoi(c.Query("limit", "50"))

	logs, err := h.service.GetAuditLog(c.Context(), entidade, entidadeID, limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"audit_log": logs})
}

func (h *Handler) RecordDecision(c *fiber.Ctx) error {
	var req models.RecordDecisionRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "payload inválido"})
	}

	decision, err := h.service.RecordDecision(c.Context(), req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"decision": decision})
}

func (h *Handler) SupersedeDecision(c *fiber.Ctx) error {
	antigoID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID antigo inválido"})
	}

	var body struct {
		NovoID int64 `json:"novo_id"`
	}
	if err := c.BodyParser(&body); err != nil || body.NovoID <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "novo_id é obrigatório"})
	}

	if err := h.service.SupersedeDecision(c.Context(), antigoID, body.NovoID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "Decisão substituída com sucesso"})
}

func (h *Handler) IngestCandidate(c *fiber.Ctx) error {
	var req models.IngestCandidateRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "payload inválido"})
	}

	cand, err := h.service.IngestCandidate(c.Context(), req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"candidate": cand})
}

// MCP JSON-RPC Types
type mcpRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

type mcpResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}

func (h *Handler) HandleMCPInfo(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"name":        "janus-brain-mcp",
		"version":     "1.0.0",
		"protocol":    "mcp-jsonrpc-2.0",
		"description": "Cérebro Operacional Janus com RAG Híbrido, Wikis e pgvector",
		"tools_count": 12,
	})
}

// RunMCPStdio executa o loop MCP sobre Stdio para agy, Claude Code e Cursor
func (h *Handler) RunMCPStdio(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)
	// Buffer de até 10MB para payloads grandes de Wiki
	buf := make([]byte, 10*1024*1024)
	scanner.Buffer(buf, len(buf))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req mcpRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			respBytes, _ := json.Marshal(mcpResponse{
				JSONRPC: "2.0",
				ID:      nil,
				Error:   fiber.Map{"code": -32700, "message": "Parse error"},
			})
			os.Stdout.Write(respBytes)
			os.Stdout.WriteString("\n")
			continue
		}

		// Notificações JSON-RPC (sem ID ou com prefixo notifications/) não devem receber resposta
		if req.ID == nil || strings.HasPrefix(req.Method, "notifications/") {
			continue
		}

		resp := h.processMCPRequest(ctx, req)
		respBytes, _ := json.Marshal(resp)
		os.Stdout.Write(respBytes)
		os.Stdout.WriteString("\n")
	}

	return scanner.Err()
}

func (h *Handler) processMCPRequest(ctx context.Context, req mcpRequest) mcpResponse {
	switch req.Method {
	case "initialize":
		return mcpResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: fiber.Map{
				"protocolVersion": "2024-11-05",
				"serverInfo": fiber.Map{
					"name":    "janus-core-mcp",
					"version": "1.0.0",
				},
				"capabilities": fiber.Map{
					"tools": fiber.Map{},
				},
			},
		}

	case "tools/list":
		return mcpResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: fiber.Map{
				"tools": []fiber.Map{
					{
						"name":        "search_knowledge",
						"description": "Busca híbrida semântica e léxica em decisões, regras, conhecimento e wikis de clientes",
						"inputSchema": fiber.Map{
							"type": "object",
							"properties": fiber.Map{
								"query":   fiber.Map{"type": "string", "description": "Termo de busca conceitual ou código exato"},
								"sistema": fiber.Map{"type": "string", "description": "Filtro opcional de sistema/domínio/cliente (ex: winthor, cliente_x)"},
								"tipo":    fiber.Map{"type": "string", "description": "Filtro opcional de tipo (query_sql, regra, procedimento, artigo/wiki, faq)"},
							},
							"required": []string{"query"},
						},
					},
					{
						"name":        "get_knowledge",
						"description": "Recupera item específico por ID ou título, trazendo árvore de itens relacionados, regras e histórico de versões",
						"inputSchema": fiber.Map{
							"type": "object",
							"properties": fiber.Map{
								"id":                fiber.Map{"type": "integer", "description": "ID exato do item"},
								"titulo":            fiber.Map{"type": "string", "description": "Título parcial ou exato"},
								"incluir_historico": fiber.Map{"type": "boolean", "description": "Se true, traz a cadeia de versões anteriores/posteriores"},
							},
						},
					},
					{
						"name":        "save_knowledge",
						"description": "Salva um item de conhecimento, regra ou Artigo de Wiki para documentar clientes e projetos",
						"inputSchema": fiber.Map{
							"type": "object",
							"properties": fiber.Map{
								"sistema":          fiber.Map{"type": "string", "description": "Domínio ou cliente (ex: winthor, gestao_pessoas, cliente_alpha)"},
								"tipo":             fiber.Map{"type": "string", "description": "Tipo: 'artigo' (para Wiki/Documentação), 'query_sql', 'regra', 'procedimento', 'faq'"},
								"titulo":           fiber.Map{"type": "string", "description": "Título claro do item ou da Wiki"},
								"conteudo":         fiber.Map{"type": "string", "description": "Conteúdo integral em Markdown, código ou passos"},
								"descricao":        fiber.Map{"type": "string", "description": "Resumo contextual"},
								"rule_ids":         fiber.Map{"type": "array", "items": fiber.Map{"type": "integer"}, "description": "IDs de regras vinculadas"},
								"relacionados_ids": fiber.Map{"type": "array", "items": fiber.Map{"type": "integer"}, "description": "IDs de outros itens atômicos que esta Wiki costura"},
								"procedencia":      fiber.Map{"type": "string", "enum": []string{"verificado_query", "confirmado_pessoa", "hipotese"}},
								"snapshot_em":      fiber.Map{"type": "string", "description": "Data do levantamento do fato (AAAA-MM-DD)"},
								"reverificar_apos": fiber.Map{"type": "string", "description": "Data sugerida para revisão (AAAA-MM-DD)"},
							},
							"required": []string{"sistema", "tipo", "titulo", "conteudo", "descricao"},
						},
					},
					{
						"name":        "list_knowledge",
						"description": "Lista itens de conhecimento ou páginas de Wiki com paginação e filtros por domínio",
						"inputSchema": fiber.Map{
							"type": "object",
							"properties": fiber.Map{
								"sistema": fiber.Map{"type": "string", "description": "Filtrar por slug do domínio/cliente"},
								"tipo":    fiber.Map{"type": "string", "description": "Filtrar por tipo (ex: 'artigo' para ver só Wikis)"},
								"status":  fiber.Map{"type": "string", "description": "'ativo' (padrão), 'substituido' ou 'todos'"},
								"limit":   fiber.Map{"type": "integer", "description": "Quantidade de itens (padrão 20)"},
								"offset":  fiber.Map{"type": "integer", "description": "Deslocamento de página"},
							},
						},
					},
					{
						"name":        "list_knowledge_domains",
						"description": "Lista todos os domínios/clientes cadastrados com contadores de itens ativos e substituídos",
						"inputSchema": fiber.Map{"type": "object"},
					},
					{
						"name":        "record_decision",
						"description": "Registra uma decisão literal de negócio com autoria e canal de origem",
						"inputSchema": fiber.Map{
							"type": "object",
							"properties": fiber.Map{
								"texto":           fiber.Map{"type": "string", "description": "Texto literal da decisão"},
								"autor":           fiber.Map{"type": "string", "description": "Pessoa que tomou a decisão"},
								"grupo":           fiber.Map{"type": "string", "description": "Grupo ou canal de origem"},
								"wpp_message_id":  fiber.Map{"type": "string", "description": "ID da mensagem"},
								"wpp_message_url": fiber.Map{"type": "string", "description": "Link para a mensagem"},
								"tema":            fiber.Map{"type": "array", "items": fiber.Map{"type": "string"}},
							},
							"required": []string{"texto", "autor", "grupo", "wpp_message_id"},
						},
					},
					{
						"name":        "supersede_decision",
						"description": "Atualiza uma decisão marcando a versão anterior como substituída de forma não-destrutiva",
						"inputSchema": fiber.Map{
							"type": "object",
							"properties": fiber.Map{
								"antigo_id": fiber.Map{"type": "integer", "description": "ID da decisão anterior"},
								"novo_id":   fiber.Map{"type": "integer", "description": "ID da nova decisão vigente"},
							},
							"required": []string{"antigo_id", "novo_id"},
						},
					},
					{
						"name":        "supersede_knowledge",
						"description": "Substitui uma versão anterior de um artigo/item de conhecimento por uma versão atualizada",
						"inputSchema": fiber.Map{
							"type": "object",
							"properties": fiber.Map{
								"antigo_id": fiber.Map{"type": "integer", "description": "ID do item que está sendo substituído"},
								"sistema":   fiber.Map{"type": "string"},
								"tipo":      fiber.Map{"type": "string"},
								"titulo":    fiber.Map{"type": "string"},
								"conteudo":  fiber.Map{"type": "string"},
								"descricao": fiber.Map{"type": "string"},
							},
							"required": []string{"antigo_id", "sistema", "tipo", "titulo", "conteudo", "descricao"},
						},
					},
					{
						"name":        "delete_knowledge",
						"description": "Remove um item de conhecimento ou página de Wiki, gravando snapshot na auditoria",
						"inputSchema": fiber.Map{
							"type": "object",
							"properties": fiber.Map{
								"id": fiber.Map{"type": "integer", "description": "ID do item a ser removido"},
							},
							"required": []string{"id"},
						},
					},
					{
						"name":        "merge_domains",
						"description": "Funde dois domínios/clientes, movendo todos os itens de origem para destino",
						"inputSchema": fiber.Map{
							"type": "object",
							"properties": fiber.Map{
								"origem":  fiber.Map{"type": "string", "description": "Slug do domínio a ser unificado"},
								"destino": fiber.Map{"type": "string", "description": "Slug do domínio definitivo"},
							},
							"required": []string{"origem", "destino"},
						},
					},
					{
						"name":        "get_audit_log",
						"description": "Consulta a trilha de auditoria append-only de alterações e criação de conhecimento",
						"inputSchema": fiber.Map{
							"type": "object",
							"properties": fiber.Map{
								"entidade":    fiber.Map{"type": "string", "description": "'knowledge_item' ou 'knowledge_domain'"},
								"entidade_id": fiber.Map{"type": "string", "description": "ID específico da entidade"},
								"limit":       fiber.Map{"type": "integer", "description": "Quantidade máxima de registros"},
							},
						},
					},
					{
						"name":        "get_wiki",
						"description": "Carrega uma Wiki ou Artigo completo com todos os seus subtópicos e itens filhos conectados",
						"inputSchema": fiber.Map{
							"type": "object",
							"properties": fiber.Map{
								"id":     fiber.Map{"type": "integer", "description": "ID do artigo de Wiki"},
								"titulo": fiber.Map{"type": "string", "description": "Título da Wiki a carregar"},
							},
						},
					},
				},
			},
		}

	case "tools/call":
		toolName, _ := req.Params["name"].(string)
		args, _ := req.Params["arguments"].(map[string]any)

		switch toolName {
		case "search_knowledge":
			query, _ := args["query"].(string)
			sistema, _ := args["sistema"].(string)
			tipo, _ := args["tipo"].(string)
			hits, err := h.search.Search(ctx, query, search.FilterOptions{Sistema: sistema, Tipo: tipo})
			if err != nil {
				return mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: fiber.Map{"code": -32603, "message": err.Error()}}
			}
			return formatMCPPayload(req.ID, hits)

		case "get_knowledge", "get_wiki":
			var input models.GetKnowledgeInput
			if rawID, ok := args["id"].(float64); ok {
				idInt := int64(rawID)
				input.ID = &idInt
			}
			if titulo, ok := args["titulo"].(string); ok {
				input.Titulo = &titulo
			}
			if hist, ok := args["incluir_historico"].(bool); ok {
				input.IncluirHistorico = hist
			}
			items, err := h.service.GetKnowledge(ctx, input)
			if err != nil {
				return mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: fiber.Map{"code": -32603, "message": err.Error()}}
			}
			return formatMCPPayload(req.ID, items)

		case "save_knowledge":
			var saveReq models.SaveKnowledgeRequest
			argBytes, _ := json.Marshal(args)
			_ = json.Unmarshal(argBytes, &saveReq)

			item, domain, err := h.service.SaveKnowledge(ctx, saveReq)
			if err != nil {
				return mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: fiber.Map{"code": -32603, "message": err.Error()}}
			}
			return formatMCPPayload(req.ID, fiber.Map{"item": item, "domain": domain})

		case "list_knowledge":
			sistema, _ := args["sistema"].(string)
			tipo, _ := args["tipo"].(string)
			status, _ := args["status"].(string)
			limit := 20
			if rawLim, ok := args["limit"].(float64); ok && rawLim > 0 {
				limit = int(rawLim)
			}
			offset := 0
			if rawOff, ok := args["offset"].(float64); ok && rawOff > 0 {
				offset = int(rawOff)
			}
			items, err := h.service.ListKnowledge(ctx, sistema, tipo, status, limit, offset)
			if err != nil {
				return mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: fiber.Map{"code": -32603, "message": err.Error()}}
			}
			return formatMCPPayload(req.ID, items)

		case "list_knowledge_domains":
			domains, err := h.service.ListDomains(ctx)
			if err != nil {
				return mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: fiber.Map{"code": -32603, "message": err.Error()}}
			}
			return formatMCPPayload(req.ID, domains)

		case "record_decision":
			var decReq models.RecordDecisionRequest
			argBytes, _ := json.Marshal(args)
			_ = json.Unmarshal(argBytes, &decReq)

			dec, err := h.service.RecordDecision(ctx, decReq)
			if err != nil {
				return mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: fiber.Map{"code": -32603, "message": err.Error()}}
			}
			return formatMCPPayload(req.ID, dec)

		case "supersede_decision":
			antigoID := int64(args["antigo_id"].(float64))
			novoID := int64(args["novo_id"].(float64))
			if err := h.service.SupersedeDecision(ctx, antigoID, novoID); err != nil {
				return mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: fiber.Map{"code": -32603, "message": err.Error()}}
			}
			return formatMCPPayload(req.ID, fiber.Map{"status": "ok", "message": "Decisão substituída com sucesso"})

		case "supersede_knowledge":
			antigoID := int64(args["antigo_id"].(float64))
			var novoReq models.SaveKnowledgeRequest
			argBytes, _ := json.Marshal(args)
			_ = json.Unmarshal(argBytes, &novoReq)

			novoItem, err := h.service.SupersedeKnowledge(ctx, antigoID, novoReq, "mcp-user")
			if err != nil {
				return mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: fiber.Map{"code": -32603, "message": err.Error()}}
			}
			return formatMCPPayload(req.ID, fiber.Map{"status": "ok", "novo_item": novoItem})

		case "delete_knowledge":
			id := int64(args["id"].(float64))
			if err := h.service.DeleteKnowledge(ctx, id, "mcp-user"); err != nil {
				return mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: fiber.Map{"code": -32603, "message": err.Error()}}
			}
			return formatMCPPayload(req.ID, fiber.Map{"status": "ok", "message": "Item removido e arquivado em auditoria"})

		case "merge_domains":
			origem, _ := args["origem"].(string)
			destino, _ := args["destino"].(string)
			res, err := h.service.MergeDomains(ctx, origem, destino, "mcp-user")
			if err != nil {
				return mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: fiber.Map{"code": -32603, "message": err.Error()}}
			}
			return formatMCPPayload(req.ID, res)

		case "get_audit_log":
			entidade, _ := args["entidade"].(string)
			entidadeID, _ := args["entidade_id"].(string)
			limit := 50
			if rawLim, ok := args["limit"].(float64); ok && rawLim > 0 {
				limit = int(rawLim)
			}
			logs, err := h.service.GetAuditLog(ctx, entidade, entidadeID, limit)
			if err != nil {
				return mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: fiber.Map{"code": -32603, "message": err.Error()}}
			}
			return formatMCPPayload(req.ID, logs)

		default:
			return mcpResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   fiber.Map{"code": -32601, "message": fmt.Sprintf("Tool desconhecida: %s", toolName)},
			}
		}

	default:
		return mcpResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   fiber.Map{"code": -32601, "message": "Método não encontrado"},
		}
	}
}

func formatMCPPayload(id any, payload any) mcpResponse {
	jsonBytes, _ := json.MarshalIndent(payload, "", "  ")
	return mcpResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: fiber.Map{
			"content": []fiber.Map{
				{"type": "text", "text": string(jsonBytes)},
			},
		},
	}
}

func (h *Handler) HandleMCP(c *fiber.Ctx) error {
	var req mcpRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(mcpResponse{
			JSONRPC: "2.0",
			ID:      nil,
			Error:   fiber.Map{"code": -32700, "message": "Parse error"},
		})
	}

	resp := h.processMCPRequest(c.Context(), req)
	return c.JSON(resp)
}
