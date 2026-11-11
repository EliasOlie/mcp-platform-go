package eosecurity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

type Identity struct {
	Valid       bool     `json:"valid"`
	ClientID    string   `json:"client_id"`
	TenantID    string   `json:"tenant_id"`
	ClientType  string   `json:"client_type"`
	Permissions []string `json:"permissions"`
	Roles       []string `json:"roles,omitempty"`
}

type cachedItem struct {
	ident     *Identity
	expiresAt time.Time
}

type Client struct {
	authURL    string
	httpClient *http.Client
	mu         sync.RWMutex
	cache      map[string]cachedItem
}

func New(authServiceURL string) *Client {
	return &Client{
		authURL: strings.TrimRight(authServiceURL, "/"),
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
		cache: make(map[string]cachedItem),
	}
}

// Verify valida o token ou chave M2M no auth-service com cache de 30s
func (c *Client) Verify(ctx context.Context, token string) (*Identity, error) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	if trimmed == "" {
		return nil, fmt.Errorf("token não fornecido")
	}

	c.mu.RLock()
	if item, found := c.cache[trimmed]; found && time.Now().Before(item.expiresAt) {
		c.mu.RUnlock()
		return item.ident, nil
	}
	c.mu.RUnlock()

	body, _ := json.Marshal(map[string]string{"token": trimmed})
	req, err := http.NewRequestWithContext(ctx, "POST", c.authURL+"/auth/verify", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+trimmed)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth service indisponível: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("credencial inválida (status %d)", resp.StatusCode)
	}

	var ident Identity
	if err := json.NewDecoder(resp.Body).Decode(&ident); err != nil || !ident.Valid {
		return nil, fmt.Errorf("credencial revogada ou inválida")
	}

	c.mu.Lock()
	c.cache[trimmed] = cachedItem{
		ident:     &ident,
		expiresAt: time.Now().Add(30 * time.Second),
	}
	c.mu.Unlock()

	return &ident, nil
}

// FiberMiddleware retorna um middleware pronto para Fiber que injeta a Identity no Context
func (c *Client) FiberMiddleware(requiredScope string) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		authHeader := ctx.Get("Authorization")
		if authHeader == "" {
			authHeader = ctx.Get("X-API-Key")
		}

		ident, err := c.Verify(ctx.Context(), authHeader)
		if err != nil {
			return ctx.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error":   "unauthorized",
				"message": err.Error(),
			})
		}

		if requiredScope != "" && !ident.HasPermission(requiredScope) {
			return ctx.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":          "forbidden",
				"scope_required": requiredScope,
			})
		}

		ctx.Locals("auth_identity", ident)
		return ctx.Next()
	}
}

func (i *Identity) HasPermission(required string) bool {
	for _, p := range i.Permissions {
		if p == "*" || p == required {
			return true
		}
		if strings.HasSuffix(p, ":*") && strings.HasPrefix(required, strings.TrimSuffix(p, ":*")+":") {
			return true
		}
	}
	return false
}
