package authclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Identity struct {
	Valid       bool     `json:"valid"`
	ClientID    string   `json:"client_id"`
	TenantID    string   `json:"tenant_id"`
	ClientType  string   `json:"client_type"`
	Permissions []string `json:"permissions"`
	Roles       []string `json:"roles,omitempty"`
	ExpiresAt   int64    `json:"expires_at,omitempty"`
}

type cachedItem struct {
	identity  *Identity
	expiresAt time.Time
}

type Client struct {
	authURL    string
	vpsqlURL   string
	httpClient *http.Client
	cacheMu    sync.RWMutex
	cache      map[string]cachedItem
}

func NewClient(authURL, vpsqlURL string) *Client {
	return &Client{
		authURL:  strings.TrimRight(authURL, "/"),
		vpsqlURL: strings.TrimRight(vpsqlURL, "/"),
		httpClient: &http.Client{
			Timeout: 4 * time.Second,
		},
		cache: make(map[string]cachedItem),
	}
}

// Verify consulta o auth-service ou VPSQL com cache in-memory de 30s
func (c *Client) Verify(ctx context.Context, token string) (*Identity, error) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	if trimmed == "" {
		return nil, fmt.Errorf("token não fornecido")
	}

	// 1. Checa cache in-memory
	c.cacheMu.RLock()
	if item, found := c.cache[trimmed]; found && time.Now().Before(item.expiresAt) {
		c.cacheMu.RUnlock()
		return item.identity, nil
	}
	c.cacheMu.RUnlock()

	// 2. Determina endpoint: se for chave do VPSQL (vpsql_sk_...), valida no VPSQL; caso contrário, no auth-service
	var verifyEndpoint string
	if strings.HasPrefix(trimmed, "vpsql_sk_") {
		verifyEndpoint = c.vpsqlURL + "/v1/auth/verify"
	} else {
		verifyEndpoint = c.authURL + "/auth/verify"
	}

	reqBody, _ := json.Marshal(map[string]string{"token": trimmed})
	req, err := http.NewRequestWithContext(ctx, "POST", verifyEndpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("erro ao criar requisição de auth: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+trimmed)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serviço de autenticação (%s) indisponível: %w", verifyEndpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("credencial inválida ou rejeitada (status %d)", resp.StatusCode)
	}

	var raw struct {
		Valid        bool     `json:"valid"`
		ClientID     string   `json:"client_id"`
		KeyID        string   `json:"key_id"`
		TenantID     string   `json:"tenant_id"`
		ClientType   string   `json:"client_type"`
		Permissions  []string `json:"permissions"`
		AllowedTools []string `json:"allowed_tools"`
		Roles        []string `json:"roles"`
		ExpiresAt    int64    `json:"expires_at"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("resposta de auth inválida: %w", err)
	}

	if !raw.Valid {
		return nil, fmt.Errorf("credencial revogada ou inválida")
	}

	perms := raw.Permissions
	if len(perms) == 0 && len(raw.AllowedTools) > 0 {
		perms = raw.AllowedTools
	}

	clientID := raw.ClientID
	if clientID == "" && raw.KeyID != "" {
		clientID = raw.KeyID
	}

	ident := Identity{
		Valid:       raw.Valid,
		ClientID:    clientID,
		TenantID:    raw.TenantID,
		ClientType:  raw.ClientType,
		Permissions: perms,
		Roles:       raw.Roles,
		ExpiresAt:   raw.ExpiresAt,
	}

	// 3. Salva no cache por 30 segundos
	c.cacheMu.Lock()
	c.cache[trimmed] = cachedItem{
		identity:  &ident,
		expiresAt: time.Now().Add(30 * time.Second),
	}
	c.cacheMu.Unlock()

	return &ident, nil
}

// SatisfiesScope verifica se o usuário possui a permissão requerida
func (ident *Identity) SatisfiesScope(requiredScope string) bool {
	if ident == nil {
		return false
	}
	for _, p := range ident.Permissions {
		if p == "*" || p == "*:*" {
			return true
		}
		if p == requiredScope {
			return true
		}
		if strings.HasSuffix(p, ":*") {
			prefix := strings.TrimSuffix(p, ":*")
			if strings.HasPrefix(requiredScope, prefix+":") {
				return true
			}
		}
	}
	return false
}
