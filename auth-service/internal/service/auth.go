package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/eliasolie/core-utils/auth-service/internal/config"
	"github.com/eliasolie/core-utils/auth-service/internal/crypto"
	"github.com/eliasolie/core-utils/auth-service/internal/database"
	"github.com/eliasolie/core-utils/auth-service/internal/models"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	cfg        *config.Config
	db         *database.DB
	keyManager *crypto.KeyManager
}

func NewAuthService(cfg *config.Config, db *database.DB, km *crypto.KeyManager) *AuthService {
	return &AuthService{
		cfg:        cfg,
		db:         db,
		keyManager: km,
	}
}

func (s *AuthService) KeyManager() *crypto.KeyManager {
	return s.keyManager
}

// VerifyToken valida uma credencial (API Key ou JWT) e retorna as claims e permissões
func (s *AuthService) VerifyToken(ctx context.Context, token string) (*models.VerifyResponse, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return nil, fmt.Errorf("token ausente")
	}

	// 1. Bypass para Master Admin Key
	if s.cfg.AdminMasterKey != "" && crypto.ConstantTimeCompareHash(crypto.HashKey(trimmed), crypto.HashKey(s.cfg.AdminMasterKey)) {
		return &models.VerifyResponse{
			Valid:       true,
			ClientID:    "master-admin",
			TenantID:    "admin",
			ClientType:  "m2m",
			Permissions: []string{"*"},
			Roles:       []string{"admin"},
			ExpiresAt:   time.Now().Add(24 * time.Hour).Unix(),
		}, nil
	}

	// 2. Chave de API M2M do VPSQL (prefixo vpsql_sk_)
	if strings.HasPrefix(trimmed, "vpsql_sk_") {
		return s.verifyWithVPSQL(ctx, trimmed)
	}

	// 3. Chave de API M2M local (prefixo eo_live_)
	if strings.HasPrefix(trimmed, "eo_live_") {
		return s.verifyAPIKey(ctx, trimmed)
	}

	// 4. JWT EdDSA
	if strings.Count(trimmed, ".") == 2 {
		return s.verifyJWT(trimmed)
	}

	// Fallback: tenta buscar localmente e depois no VPSQL
	resp, err := s.verifyAPIKey(ctx, trimmed)
	if err == nil && resp != nil {
		return resp, nil
	}
	return s.verifyWithVPSQL(ctx, trimmed)
}

func (s *AuthService) verifyAPIKey(ctx context.Context, rawKey string) (*models.VerifyResponse, error) {
	keyHash := crypto.HashKey(rawKey)

	query := `
		SELECT k.id, k.tenant_id, k.name, k.permissions, k.is_active, k.expires_at, t.status
		FROM auth_api_keys k
		JOIN auth_tenants t ON t.id = k.tenant_id
		WHERE k.key_hash = $1
	`
	var keyID, tenantID, name, tenantStatus string
	var perms []string
	var isActive bool
	var expiresAt *time.Time

	err := s.db.Pool.QueryRow(ctx, query, keyHash).Scan(
		&keyID, &tenantID, &name, &perms, &isActive, &expiresAt, &tenantStatus,
	)
	if err != nil {
		return nil, fmt.Errorf("chave de API inválida ou não encontrada")
	}

	if !isActive {
		return nil, fmt.Errorf("chave de API '%s' suspensa", name)
	}
	if tenantStatus != "active" {
		return nil, fmt.Errorf("tenant '%s' suspenso", tenantID)
	}
	if expiresAt != nil && expiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("chave de API '%s' expirada", name)
	}

	// Atualização assíncrona de estatísticas para latência < 1ms
	go func(id string) {
		_, _ = s.db.Pool.Exec(context.Background(),
			`UPDATE auth_api_keys SET total_requests = total_requests + 1, last_used_at = NOW() WHERE id = $1`, id)
	}(keyID)

	var expEpoch int64 = 0
	if expiresAt != nil {
		expEpoch = expiresAt.Unix()
	}

	return &models.VerifyResponse{
		Valid:       true,
		ClientID:    keyID,
		TenantID:    tenantID,
		ClientType:  "m2m",
		Permissions: perms,
		Roles:       []string{"service"},
		ExpiresAt:   expEpoch,
	}, nil
}

func (s *AuthService) verifyJWT(token string) (*models.VerifyResponse, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("formato JWT inválido")
	}

	headerB64, claimsB64, sigB64 := parts[0], parts[1], parts[2]
	signingInput := fmt.Sprintf("%s.%s", headerB64, claimsB64)

	signature, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, fmt.Errorf("assinatura base64 inválida: %w", err)
	}

	pubKey := s.keyManager.PublicKey()
	if !ed25519.Verify(pubKey, []byte(signingInput), signature) {
		return nil, fmt.Errorf("assinatura do token inválida")
	}

	claimsBytes, err := base64.RawURLEncoding.DecodeString(claimsB64)
	if err != nil {
		return nil, fmt.Errorf("claims base64 inválido: %w", err)
	}

	var claims models.TokenClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, fmt.Errorf("json de claims inválido: %w", err)
	}

	if claims.Exp > 0 && claims.Exp < time.Now().Unix() {
		return nil, fmt.Errorf("token expirado")
	}

	return &models.VerifyResponse{
		Valid:       true,
		ClientID:    claims.Sub,
		TenantID:    claims.TenantID,
		Email:       claims.Email,
		ClientType:  claims.ClientType,
		Permissions: claims.Permissions,
		Roles:       claims.Roles,
		ExpiresAt:   claims.Exp,
	}, nil
}

// CreateTenant provisiona um novo cliente ou ambiente
func (s *AuthService) CreateTenant(ctx context.Context, req models.CreateTenantRequest) (*models.Tenant, error) {
	id := strings.ToLower(strings.TrimSpace(req.ID))
	if id == "" {
		return nil, fmt.Errorf("o campo 'id' do tenant é obrigatório")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = id
	}

	metaJSON, _ := json.Marshal(req.Metadata)

	query := `
		INSERT INTO auth_tenants (id, name, status, metadata)
		VALUES ($1, $2, 'active', $3)
		RETURNING id, name, status, created_at, updated_at
	`
	var t models.Tenant
	err := s.db.Pool.QueryRow(ctx, query, id, name, metaJSON).Scan(
		&t.ID, &t.Name, &t.Status, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("falha ao criar tenant: %w", err)
	}
	t.Metadata = req.Metadata
	return &t, nil
}

// ListTenants lista todos os clientes cadastrados com contagem de usuários
func (s *AuthService) ListTenants(ctx context.Context) ([]models.Tenant, error) {
	query := `
		SELECT t.id, t.name, t.status, t.created_at, t.updated_at, COUNT(u.id) as user_count
		FROM auth_tenants t
		LEFT JOIN auth_users u ON u.tenant_id = t.id
		GROUP BY t.id, t.name, t.status, t.created_at, t.updated_at
		ORDER BY t.name ASC
	`
	rows, err := s.db.Pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tenants []models.Tenant
	for rows.Next() {
		var t models.Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Status, &t.CreatedAt, &t.UpdatedAt, &t.UserCount); err == nil {
			tenants = append(tenants, t)
		}
	}
	return tenants, nil
}

// EnsureBootstrapAdmin garante que o tenant 'admin' e o usuário master inicial existam no banco
func (s *AuthService) EnsureBootstrapAdmin(ctx context.Context) error {
	// 1. Garante que o tenant 'admin' exista
	_, _ = s.db.Pool.Exec(ctx, `
		INSERT INTO auth_tenants (id, name, status)
		VALUES ('admin', 'Administração Geral EliasOlie', 'active')
		ON CONFLICT (id) DO NOTHING
	`)

	// 2. Verifica se já existe algum usuário no tenant 'admin'
	var hasAdmin bool
	err := s.db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM auth_users WHERE tenant_id = 'admin')`).Scan(&hasAdmin)
	if err != nil {
		return err
	}

	if hasAdmin {
		return nil
	}

	// 3. Cria o usuário master inicial idempotente
	email := strings.ToLower(strings.TrimSpace(s.cfg.BootstrapAdminEmail))
	password := s.cfg.BootstrapAdminPassword
	if email == "" || password == "" {
		return nil
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}

	userID := uuid.New().String()
	now := time.Now()
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO auth_users (id, tenant_id, email, password_hash, roles, permissions, is_active, created_at, updated_at)
		VALUES ($1, 'admin', $2, $3, '{"admin"}', '{"*"}', true, $4, $4)
		ON CONFLICT (email) DO NOTHING
	`, userID, email, string(hashed), now)

	if err == nil {
		log.Printf("[Auth-Bootstrap] Usuário master criado com sucesso: %s (Tenant: admin, Role: admin)", email)
	}

	return err
}

// CreateUser cadastra um novo usuário para um cliente/tenant com senha criptografada via bcrypt
func (s *AuthService) CreateUser(ctx context.Context, req models.CreateUserRequest) (*models.UserResponse, error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		return nil, fmt.Errorf("email é obrigatório")
	}
	if len(req.Password) < 6 {
		return nil, fmt.Errorf("a senha deve ter no mínimo 6 caracteres")
	}
	tenantID := strings.TrimSpace(req.TenantID)
	if tenantID == "" {
		tenantID = "default"
	}

	// 1. Verifica se o tenant existe
	var exists bool
	err := s.db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM auth_tenants WHERE id = $1)`, tenantID).Scan(&exists)
	if err != nil || !exists {
		return nil, fmt.Errorf("cliente/tenant '%s' não encontrado", tenantID)
	}

	// 2. Hash da senha com bcrypt (custo 12)
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		return nil, fmt.Errorf("falha ao criptografar senha: %w", err)
	}

	roles := req.Roles
	if len(roles) == 0 {
		roles = []string{"user"}
	}
	perms := req.Permissions
	if perms == nil {
		perms = []string{}
	}

	userID := uuid.New().String()
	now := time.Now()

	query := `
		INSERT INTO auth_users (id, tenant_id, email, password_hash, roles, permissions, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, true, $7, $7)
		RETURNING id, tenant_id, email, roles, permissions, is_active, created_at
	`
	var u models.UserResponse
	err = s.db.Pool.QueryRow(ctx, query, userID, tenantID, email, string(hashed), roles, perms, now).Scan(
		&u.ID, &u.TenantID, &u.Email, &u.Roles, &u.Permissions, &u.IsActive, &u.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			return nil, fmt.Errorf("o email '%s' já está cadastrado", email)
		}
		return nil, fmt.Errorf("erro ao criar usuário: %w", err)
	}

	return &u, nil
}

// ListUsers lista os usuários cadastrados, opcionalmente filtrando por tenant
func (s *AuthService) ListUsers(ctx context.Context, tenantID string) ([]models.UserResponse, error) {
	query := `
		SELECT id, tenant_id, email, roles, permissions, is_active, created_at
		FROM auth_users
		WHERE ($1 = '' OR tenant_id = $1)
		ORDER BY created_at DESC
	`
	rows, err := s.db.Pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.UserResponse
	for rows.Next() {
		var u models.UserResponse
		if err := rows.Scan(
			&u.ID, &u.TenantID, &u.Email, &u.Roles, &u.Permissions, &u.IsActive, &u.CreatedAt,
		); err == nil {
			users = append(users, u)
		}
	}
	return users, nil
}

// DeleteUser remove um usuário do sistema
func (s *AuthService) DeleteUser(ctx context.Context, userID string) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM auth_users WHERE id = $1`, userID)
	return err
}

// ToggleUser ativa ou desativa o acesso de um usuário
func (s *AuthService) ToggleUser(ctx context.Context, userID string) (*models.UserResponse, error) {
	query := `
		UPDATE auth_users
		SET is_active = NOT is_active, updated_at = NOW()
		WHERE id = $1
		RETURNING id, tenant_id, email, roles, permissions, is_active, created_at
	`
	var u models.UserResponse
	err := s.db.Pool.QueryRow(ctx, query, userID).Scan(
		&u.ID, &u.TenantID, &u.Email, &u.Roles, &u.Permissions, &u.IsActive, &u.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("usuário não encontrado")
	}
	return &u, nil
}

// Login autentica um usuário via email e senha e devolve um JWT Ed25519
func (s *AuthService) Login(ctx context.Context, req models.LoginRequest) (*models.LoginResponse, error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || req.Password == "" {
		return nil, fmt.Errorf("email e senha são obrigatórios")
	}

	query := `
		SELECT u.id, u.tenant_id, u.email, u.password_hash, u.roles, u.permissions, u.is_active, u.created_at, t.status
		FROM auth_users u
		JOIN auth_tenants t ON t.id = u.tenant_id
		WHERE lower(u.email) = $1
	`
	var u models.User
	var tenantStatus string
	err := s.db.Pool.QueryRow(ctx, query, email).Scan(
		&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Roles, &u.Permissions, &u.IsActive, &u.CreatedAt, &tenantStatus,
	)
	if err != nil {
		return nil, fmt.Errorf("credenciais inválidas")
	}

	if !u.IsActive {
		return nil, fmt.Errorf("este acesso de usuário está inativo/suspenso")
	}
	if tenantStatus != "active" {
		return nil, fmt.Errorf("o cliente/tenant '%s' está suspenso", u.TenantID)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)); err != nil {
		return nil, fmt.Errorf("credenciais inválidas")
	}

	// Emite o JWT assinado com Ed25519
	ttl := 24 * time.Hour
	now := time.Now()
	claims := models.TokenClaims{
		Sub:         u.ID,
		TenantID:    u.TenantID,
		ClientType:  "human",
		Email:       u.Email,
		Roles:       u.Roles,
		Permissions: u.Permissions,
		Iss:         s.cfg.Issuer,
		Aud:         s.cfg.Audience,
		Iat:         now.Unix(),
		Exp:         now.Add(ttl).Unix(),
		Jti:         uuid.New().String(),
	}

	claimsBytes, err := json.Marshal(claims)
	if err != nil {
		return nil, err
	}

	tokenStr, err := s.keyManager.SignToken(claimsBytes)
	if err != nil {
		return nil, fmt.Errorf("falha ao assinar token de acesso: %w", err)
	}

	return &models.LoginResponse{
		Token:     tokenStr,
		TokenType: "Bearer",
		ExpiresIn: int64(ttl.Seconds()),
		User: models.UserResponse{
			ID:          u.ID,
			TenantID:    u.TenantID,
			Email:       u.Email,
			Roles:       u.Roles,
			Permissions: u.Permissions,
			IsActive:    u.IsActive,
			CreatedAt:   u.CreatedAt,
		},
	}, nil
}

// CreateAPIKey gera uma nova chave de API com escopos específicos
func (s *AuthService) CreateAPIKey(ctx context.Context, req models.CreateAPIKeyRequest) (*models.APIKey, error) {
	tenantID := strings.TrimSpace(req.TenantID)
	if tenantID == "" {
		tenantID = "default"
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Default API Key"
	}

	perms := req.Permissions
	if len(perms) == 0 {
		perms = []string{"*"}
	}

	rpm := req.RateLimitRPM
	if rpm <= 0 {
		rpm = 120
	}

	rawKey, prefix, keyHash := crypto.GenerateAPIKey()
	id := uuid.New().String()

	var expiresAt *time.Time
	if req.ExpiresInDays > 0 {
		exp := time.Now().AddDate(0, 0, req.ExpiresInDays)
		expiresAt = &exp
	}

	// Garante que o tenant existe (auto-criação)
	_, _ = s.db.Pool.Exec(ctx, `INSERT INTO auth_tenants (id, name, status) VALUES ($1, $1, 'active') ON CONFLICT (id) DO NOTHING`, tenantID)

	query := `
		INSERT INTO auth_api_keys (id, tenant_id, name, key_prefix, key_hash, permissions, rate_limit_rpm, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, tenant_id, name, key_prefix, permissions, is_active, rate_limit_rpm, total_requests, created_at
	`
	var k models.APIKey
	err := s.db.Pool.QueryRow(ctx, query, id, tenantID, name, prefix, keyHash, perms, rpm, expiresAt).Scan(
		&k.ID, &k.TenantID, &k.Name, &k.KeyPrefix, &k.Permissions, &k.IsActive, &k.RateLimitRPM, &k.TotalRequests, &k.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("falha ao salvar chave de API: %w", err)
	}
	k.RawKey = rawKey // Devolvida exclusivamente na resposta de criação
	k.ExpiresAt = expiresAt

	log.Printf("[Auth] Nova chave provisionada: ID=%s Tenant=%s Name=%s Prefix=%s", k.ID, k.TenantID, k.Name, k.KeyPrefix)
	return &k, nil
}

// ListAPIKeys lista as chaves ativas de um tenant
func (s *AuthService) ListAPIKeys(ctx context.Context, tenantID string) ([]models.APIKey, error) {
	query := `
		SELECT id, tenant_id, name, key_prefix, permissions, is_active, rate_limit_rpm, total_requests, last_used_at, expires_at, created_at
		FROM auth_api_keys
		WHERE ($1 = '' OR tenant_id = $1)
		ORDER BY created_at DESC
	`
	rows, err := s.db.Pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []models.APIKey
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(
			&k.ID, &k.TenantID, &k.Name, &k.KeyPrefix, &k.Permissions, &k.IsActive,
			&k.RateLimitRPM, &k.TotalRequests, &k.LastUsedAt, &k.ExpiresAt, &k.CreatedAt,
		); err == nil {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// IssueJWTFromKey emite um JWT EdDSA a partir de uma API Key válida
func (s *AuthService) IssueJWTFromKey(ctx context.Context, rawKey string, ttl time.Duration) (string, *models.TokenClaims, error) {
	ver, err := s.verifyAPIKey(ctx, rawKey)
	if err != nil {
		return "", nil, err
	}

	if ttl <= 0 {
		ttl = 1 * time.Hour
	}

	now := time.Now()
	claims := models.TokenClaims{
		Sub:         ver.ClientID,
		TenantID:    ver.TenantID,
		ClientType:  "m2m",
		Roles:       ver.Roles,
		Permissions: ver.Permissions,
		Iss:         s.cfg.Issuer,
		Aud:         s.cfg.Audience,
		Iat:         now.Unix(),
		Exp:         now.Add(ttl).Unix(),
		Jti:         uuid.New().String(),
	}

	claimsBytes, err := json.Marshal(claims)
	if err != nil {
		return "", nil, err
	}

	tokenStr, err := s.keyManager.SignToken(claimsBytes)
	if err != nil {
		return "", nil, fmt.Errorf("falha ao assinar JWT: %w", err)
	}

	return tokenStr, &claims, nil
}

func (s *AuthService) verifyWithVPSQL(ctx context.Context, rawKey string) (*models.VerifyResponse, error) {
	vpsqlURL := s.cfg.VPSQLURL
	if vpsqlURL == "" {
		vpsqlURL = "https://vpsql.example.com"
	}
	vpsqlURL = strings.TrimRight(vpsqlURL, "/")

	reqBody, _ := json.Marshal(map[string]string{"token": rawKey})
	req, err := http.NewRequestWithContext(ctx, "POST", vpsqlURL+"/v1/auth/verify", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawKey)

	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vpsql auth indisponível: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vpsql: credencial inválida ou rejeitada (status %d)", resp.StatusCode)
	}

	var raw struct {
		Valid        bool     `json:"valid"`
		ClientID     string   `json:"client_id"`
		KeyID        string   `json:"key_id"`
		KeyName      string   `json:"key_name"`
		TenantID     string   `json:"tenant_id"`
		ClientType   string   `json:"client_type"`
		Permissions  []string `json:"permissions"`
		AllowedTools []string `json:"allowed_tools"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("resposta vpsql inválida: %w", err)
	}

	if !raw.Valid {
		return nil, fmt.Errorf("vpsql: chave inválida ou inativa")
	}

	perms := raw.Permissions
	if len(perms) == 0 && len(raw.AllowedTools) > 0 {
		perms = raw.AllowedTools
	}

	clientID := raw.ClientID
	if clientID == "" && raw.KeyID != "" {
		clientID = raw.KeyID
	}

	return &models.VerifyResponse{
		Valid:       true,
		ClientID:    clientID,
		TenantID:    raw.TenantID,
		ClientType:  "vpsql_api_key",
		Permissions: perms,
		Roles:       []string{"api_user"},
		ExpiresAt:   time.Now().Add(30 * 24 * time.Hour).Unix(),
	}, nil
}
