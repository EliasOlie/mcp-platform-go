package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/eliasolie/core-utils/mcp-hub/internal/authclient"
	"github.com/eliasolie/core-utils/mcp-hub/internal/config"
	"github.com/eliasolie/core-utils/mcp-hub/internal/registry"
	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	cfg      *config.Config
	authCli  *authclient.Client
	registry *registry.Registry
}

func NewHandler(cfg *config.Config, authCli *authclient.Client, reg *registry.Registry) *Handler {
	return &Handler{
		cfg:      cfg,
		authCli:  authCli,
		registry: reg,
	}
}

func (h *Handler) RegisterRoutes(app *fiber.App) {
	app.Get("/health", h.HealthCheck)

	// RFC 9728 Discovery (Claude.ai Custom Connectors e Antigravity)
	app.Get("/.well-known/oauth-protected-resource", h.OAuthDiscovery)
	app.Get("/.well-known/oauth-protected-resource/mcp", h.OAuthDiscovery)

	// MCP Endpoint Único
	app.All("/mcp", h.HandleMCP)
}

func (h *Handler) HealthCheck(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"ok":      true,
		"service": "mcp-hub-go",
		"auth":    h.cfg.AuthServiceURL,
		"janus":   h.cfg.JanusURL,
	})
}

func (h *Handler) OAuthDiscovery(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"resource":               h.cfg.PublicURL + "/mcp",
		"authorization_servers":  []string{h.cfg.AuthServiceURL},
		"scopes_supported":       []string{"janus:*", "vpsql:*", "contratos:*"},
		"bearer_methods_supported": []string{"header"},
	})
}

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

func (h *Handler) HandleMCP(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		authHeader = c.Get("X-API-Key")
	}

	ident, err := h.authCli.Verify(c.Context(), authHeader)
	if err != nil {
		c.Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource"`, h.cfg.PublicURL))
		return c.Status(fiber.StatusUnauthorized).JSON(mcpResponse{
			JSONRPC: "2.0",
			ID:      nil,
			Error:   fiber.Map{"code": -32000, "message": fmt.Sprintf("Autenticação necessária: %v", err)},
		})
	}

	var req mcpRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(mcpResponse{
			JSONRPC: "2.0",
			ID:      nil,
			Error:   fiber.Map{"code": -32700, "message": "Parse error"},
		})
	}

	resp := h.processRequest(c.Context(), ident, req)
	return c.JSON(resp)
}

func (h *Handler) processRequest(ctx context.Context, ident *authclient.Identity, req mcpRequest) mcpResponse {
	switch req.Method {
	case "initialize":
		return mcpResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: fiber.Map{
				"protocolVersion": "2024-11-05",
				"serverInfo": fiber.Map{
					"name":    "mcp-hub-go",
					"version": "1.0.0",
					"tenant":  ident.TenantID,
				},
				"capabilities": fiber.Map{
					"tools": fiber.Map{},
				},
			},
		}

	case "tools/list":
		// Tool Gating: Retorna apenas as ferramentas que o tenant/usuário pode acessar
		visibleTools := h.registry.GetVisibleTools(ident)
		return mcpResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: fiber.Map{
				"tools": visibleTools,
			},
		}

	case "tools/call":
		toolName, _ := req.Params["name"].(string)
		args, _ := req.Params["arguments"].(map[string]any)

		res, err := h.registry.ExecuteTool(ctx, ident, toolName, args)
		if err != nil {
			return mcpResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   fiber.Map{"code": -32603, "message": err.Error()},
			}
		}

		jsonBytes, _ := json.MarshalIndent(res, "", "  ")
		return mcpResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: fiber.Map{
				"content": []fiber.Map{
					{"type": "text", "text": string(jsonBytes)},
				},
			},
		}

	default:
		return mcpResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   fiber.Map{"code": -32601, "message": "Método não encontrado"},
		}
	}
}

// RunMCPStdio executa o loop MCP em Stdio para clientes locais
func (h *Handler) RunMCPStdio(ctx context.Context, defaultToken string) error {
	scanner := bufio.NewScanner(os.Stdin)
	buf := make([]byte, 10*1024*1024)
	scanner.Buffer(buf, len(buf))

	ident, err := h.authCli.Verify(ctx, defaultToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[MCP-Hub] Aviso: Falha ao autenticar token local default: %v\n", err)
		ident = &authclient.Identity{
			Valid:       true,
			ClientID:    "local-stdio",
			TenantID:    "admin",
			Permissions: []string{"*"},
		}
	}

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

		resp := h.processRequest(ctx, ident, req)
		respBytes, _ := json.Marshal(resp)
		os.Stdout.Write(respBytes)
		os.Stdout.WriteString("\n")
	}

	return scanner.Err()
}
