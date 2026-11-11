package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/eliasolie/core-utils/mcp-hub/internal/authclient"
	"github.com/eliasolie/core-utils/mcp-hub/internal/config"
)

type ToolDefinition struct {
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	RequiredScope string         `json:"-"` // Ex: "janus:read", "vpsql:query"
	InputSchema   map[string]any `json:"inputSchema"`
}

type Registry struct {
	cfg        *config.Config
	tools      []ToolDefinition
	httpClient *http.Client
}

func NewRegistry(cfg *config.Config) *Registry {
	r := &Registry{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
	r.registerAllTools()
	return r
}

func (r *Registry) registerAllTools() {
	r.tools = []ToolDefinition{
		// 1. Módulo Janus (Cérebro Operacional & Wikis)
		{
			Name:          "janus:search_knowledge",
			Description:   "Busca híbrida semântica e léxica em decisões, regras de negócio e wikis no Janus Brain",
			RequiredScope: "janus:read",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":   map[string]any{"type": "string", "description": "Termo de busca conceitual ou código exato"},
					"sistema": map[string]any{"type": "string", "description": "Filtro opcional de cliente ou domínio"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:          "janus:get_wiki",
			Description:   "Carrega uma Wiki ou Artigo completo de projeto/cliente em Markdown",
			RequiredScope: "janus:read",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"titulo": map[string]any{"type": "string", "description": "Título parcial ou exato da Wiki"},
				},
				"required": []string{"titulo"},
			},
		},
		{
			Name:          "janus:save_knowledge",
			Description:   "Salva um item de conhecimento, regra ou Artigo de Wiki para documentar clientes e projetos",
			RequiredScope: "janus:write",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sistema":   map[string]any{"type": "string", "description": "Slug do cliente ou domínio"},
					"tipo":      map[string]any{"type": "string", "description": "'artigo' (Wiki), 'query_sql', 'regra', 'faq'"},
					"titulo":    map[string]any{"type": "string", "description": "Título claro do item"},
					"conteudo":  map[string]any{"type": "string", "description": "Conteúdo integral em Markdown ou código"},
					"descricao": map[string]any{"type": "string", "description": "Resumo contextual"},
				},
				"required": []string{"sistema", "tipo", "titulo", "conteudo", "descricao"},
			},
		},
		{
			Name:          "janus:list_domains",
			Description:   "Lista todos os domínios e clientes cadastrados no cérebro operacional",
			RequiredScope: "janus:read",
			InputSchema:   map[string]any{"type": "object"},
		},
		{
			Name:          "janus:record_decision",
			Description:   "Registra uma decisão literal de negócio com autoria e canal de origem",
			RequiredScope: "janus:write",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"texto":          map[string]any{"type": "string", "description": "Texto literal da decisão"},
					"autor":          map[string]any{"type": "string", "description": "Pessoa que tomou a decisão"},
					"grupo":          map[string]any{"type": "string", "description": "Grupo ou canal de origem"},
					"wpp_message_id": map[string]any{"type": "string", "description": "ID da mensagem"},
				},
				"required": []string{"texto", "autor", "grupo", "wpp_message_id"},
			},
		},

		// 2. Módulo VPSQL (Control Plane & AI Gateway)
		{
			Name:          "vpsql:status",
			Description:   "Retorna o status operacional do VPSQL Control Plane e AI Gateway",
			RequiredScope: "vpsql:read",
			InputSchema:   map[string]any{"type": "object"},
		},
		{
			Name:          "vpsql:list_models",
			Description:   "Lista os modelos de LLM e IA ativos no gateway do VPSQL",
			RequiredScope: "vpsql:read",
			InputSchema:   map[string]any{"type": "object"},
		},

		// 3. Módulo Contratos
		{
			Name:          "contratos:listar_templates",
			Description:   "Lista os templates de contratos e especificações disponíveis",
			RequiredScope: "contratos:read",
			InputSchema:   map[string]any{"type": "object"},
		},
	}
}

// GetVisibleTools filtra as ferramentas retornando apenas as que a identidade possui permissão
func (r *Registry) GetVisibleTools(ident *authclient.Identity) []ToolDefinition {
	var visible []ToolDefinition
	for _, t := range r.tools {
		if ident.SatisfiesScope(t.RequiredScope) {
			visible = append(visible, t)
		}
	}
	return visible
}

// ExecuteTool despacha a execução da ferramenta para o microsserviço correspondente
func (r *Registry) ExecuteTool(ctx context.Context, ident *authclient.Identity, toolName string, args map[string]any) (any, error) {
	// 1. Encontra a definição e valida permissão (Tool Gating)
	var targetTool *ToolDefinition
	for _, t := range r.tools {
		if t.Name == toolName {
			targetTool = &t
			break
		}
	}

	if targetTool == nil {
		return nil, fmt.Errorf("ferramenta '%s' não encontrada no hub", toolName)
	}

	if !ident.SatisfiesScope(targetTool.RequiredScope) {
		return nil, fmt.Errorf("permissão negada: requer escopo '%s' (suas permissões: %v)", targetTool.RequiredScope, ident.Permissions)
	}

	// 2. Despacho por Módulo
	if strings.HasPrefix(toolName, "janus:") {
		janusMethod := strings.TrimPrefix(toolName, "janus:")
		return r.forwardToJanus(ctx, janusMethod, args)
	}

	if strings.HasPrefix(toolName, "vpsql:") {
		return map[string]any{
			"status":  "online",
			"gateway": "https://vpsql.example.com",
			"tenant":  ident.TenantID,
			"model":   "@cf/meta/llama-3.3-70b-instruct-fp8-fast",
		}, nil
	}

	if strings.HasPrefix(toolName, "contratos:") {
		return map[string]any{
			"status":    "active",
			"templates": []string{"EO-Contrato-Padrao", "EO-Especificacao-Tecnica"},
		}, nil
	}

	return nil, fmt.Errorf("despachador não implementado para %s", toolName)
}

func (r *Registry) forwardToJanus(ctx context.Context, method string, args map[string]any) (any, error) {
	// Chama o Janus Core via JSON-RPC POST /mcp
	janusReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  "tools/call",
		"params": map[string]any{
			"name":      method,
			"arguments": args,
		},
	}
	bodyBytes, _ := json.Marshal(janusReq)

	url := fmt.Sprintf("%s/mcp", strings.TrimRight(r.cfg.JanusURL, "/"))
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("falha ao conectar no Janus (%s): %w", url, err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	var mcpResp struct {
		Result struct {
			Content []map[string]any `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBytes, &mcpResp); err != nil {
		return nil, fmt.Errorf("janus retornou resposta não JSON-RPC: %s", string(respBytes))
	}

	if mcpResp.Error != nil {
		return nil, fmt.Errorf("janus erro: %s", mcpResp.Error.Message)
	}

	return mcpResp.Result, nil
}
