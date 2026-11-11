package handlers

import (
	"strings"
	"time"

	"github.com/eliasolie/core-utils/auth-service/internal/config"
	"github.com/eliasolie/core-utils/auth-service/internal/models"
	"github.com/eliasolie/core-utils/auth-service/internal/service"
	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	cfg *config.Config
	svc *service.AuthService
}

func NewHandler(cfg *config.Config, svc *service.AuthService) *Handler {
	return &Handler{
		cfg: cfg,
		svc: svc,
	}
}

func (h *Handler) RegisterRoutes(app *fiber.App) {
	app.Get("/health", h.HealthCheck)

	// RFC 7517 JWKS (Chave pública Ed25519)
	app.Get("/.well-known/jwks.json", h.GetJWKS)

	// RFC 9728 OAuth Protected Resource Metadata
	app.Get("/.well-known/oauth-protected-resource", h.GetOAuthMetadata)

	auth := app.Group("/auth")
	auth.Post("/verify", h.Verify)
	auth.Post("/token", h.IssueToken)
	auth.Post("/login", h.Login)

	// Gerenciamento administrativo de chaves, clientes e usuários (requer master key ou perm 'admin:*')
	admin := auth.Group("", h.RequireMasterKey)
	admin.Post("/keys", h.CreateKey)
	admin.Get("/keys", h.ListKeys)
	admin.Post("/tenants", h.CreateTenant)
	admin.Get("/tenants", h.ListTenants)
	admin.Post("/users", h.CreateUser)
	admin.Get("/users", h.ListUsers)
	admin.Delete("/users/:id", h.DeleteUser)
	admin.Patch("/users/:id/toggle", h.ToggleUser)
}

func (h *Handler) HealthCheck(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"ok":      true,
		"service": "auth-service-go",
		"time":    time.Now().Unix(),
	})
}

func (h *Handler) GetJWKS(c *fiber.Ctx) error {
	c.Set("Cache-Control", "public, max-age=3600")
	return c.JSON(h.svc.KeyManager().GetJWKS())
}

func (h *Handler) GetOAuthMetadata(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"resource":               h.cfg.Issuer,
		"authorization_servers":  []string{h.cfg.Issuer},
		"scopes_supported":       []string{"openid", "profile", "janus:*", "vpsql:*", "contratos:*"},
		"bearer_methods_supported": []string{"header"},
	})
}

// Verify é a rota de performance crítica consumida pelo MCP-Hub, VPSQL, Janus e clientes
func (h *Handler) Verify(c *fiber.Ctx) error {
	token := c.Get("Authorization")
	if token == "" {
		// Tenta pelo body { "token": "..." }
		var body struct {
			Token string `json:"token"`
		}
		_ = c.BodyParser(&body)
		token = body.Token
	}

	cleanToken := strings.TrimPrefix(token, "Bearer ")
	if cleanToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"valid": false, "error": "token não fornecido"})
	}

	res, err := h.svc.VerifyToken(c.Context(), cleanToken)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"valid": false, "error": err.Error()})
	}

	return c.JSON(res)
}

func (h *Handler) IssueToken(c *fiber.Ctx) error {
	var req models.IssueTokenRequest
	if err := c.BodyParser(&req); err != nil || req.APIKey == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "o campo 'api_key' é obrigatório"})
	}

	tokenStr, claims, err := h.svc.IssueJWTFromKey(c.Context(), req.APIKey, 2*time.Hour)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"access_token": tokenStr,
		"token_type":   "Bearer",
		"expires_in":   7200,
		"claims":       claims,
	})
}

func (h *Handler) CreateKey(c *fiber.Ctx) error {
	var req models.CreateAPIKeyRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "payload inválido"})
	}

	key, err := h.svc.CreateAPIKey(c.Context(), req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(key)
}

func (h *Handler) ListKeys(c *fiber.Ctx) error {
	tenantID := c.Query("tenant_id")
	keys, err := h.svc.ListAPIKeys(c.Context(), tenantID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"keys": keys})
}

func (h *Handler) CreateTenant(c *fiber.Ctx) error {
	var req models.CreateTenantRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "payload inválido"})
	}

	tenant, err := h.svc.CreateTenant(c.Context(), req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(tenant)
}

func (h *Handler) ListTenants(c *fiber.Ctx) error {
	tenants, err := h.svc.ListTenants(c.Context())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"tenants": tenants})
}

func (h *Handler) Login(c *fiber.Ctx) error {
	var req models.LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "payload inválido"})
	}

	resp, err := h.svc.Login(c.Context(), req)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(resp)
}

func (h *Handler) CreateUser(c *fiber.Ctx) error {
	var req models.CreateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "payload inválido"})
	}

	user, err := h.svc.CreateUser(c.Context(), req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(user)
}

func (h *Handler) ListUsers(c *fiber.Ctx) error {
	tenantID := c.Query("tenant_id")
	users, err := h.svc.ListUsers(c.Context(), tenantID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"users": users})
}

func (h *Handler) DeleteUser(c *fiber.Ctx) error {
	id := c.Params("id")
	if err := h.svc.DeleteUser(c.Context(), id); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "deleted", "id": id})
}

func (h *Handler) ToggleUser(c *fiber.Ctx) error {
	id := c.Params("id")
	user, err := h.svc.ToggleUser(c.Context(), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(user)
}

func (h *Handler) RequireMasterKey(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	cleanKey := strings.TrimPrefix(authHeader, "Bearer ")
	if cleanKey == "" {
		cleanKey = c.Get("X-Master-Key")
	}

	if cleanKey != h.cfg.AdminMasterKey {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "requer chave de administração mestre"})
	}
	return c.Next()
}
