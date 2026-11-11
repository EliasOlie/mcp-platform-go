package models

import (
	"time"
)

type Tenant struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Status    string         `json:"status"` // 'active' | 'suspended'
	Metadata  map[string]any `json:"metadata,omitempty"`
	UserCount int            `json:"user_count,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type APIKey struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenant_id"`
	Name          string     `json:"name"`
	KeyPrefix     string     `json:"key_prefix"`
	RawKey        string     `json:"raw_key,omitempty"` // Retornado apenas na criação
	Permissions   []string   `json:"permissions"`
	IsActive      bool       `json:"is_active"`
	RateLimitRPM  int        `json:"rate_limit_rpm"`
	TotalRequests int64      `json:"total_requests"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type User struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Roles        []string  `json:"roles"`
	Permissions  []string  `json:"permissions"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type UserResponse struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Email       string    `json:"email"`
	Roles       []string  `json:"roles"`
	Permissions []string  `json:"permissions"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
}

// TokenClaims representa o payload do JWT emitido
type TokenClaims struct {
	Sub         string   `json:"sub"` // user_id ou key_id
	TenantID    string   `json:"tenant_id"`
	ClientType  string   `json:"client_type"` // "m2m" | "human"
	Email       string   `json:"email,omitempty"`
	Roles       []string `json:"roles,omitempty"`
	Permissions []string `json:"permissions"`
	Iss         string   `json:"iss"`
	Aud         string   `json:"aud"`
	Exp         int64    `json:"exp"`
	Iat         int64    `json:"iat"`
	Jti         string   `json:"jti"`
}

// VerifyResponse é o payload retornado pelo POST /auth/verify
type VerifyResponse struct {
	Valid       bool     `json:"valid"`
	ClientID    string   `json:"client_id"`
	TenantID    string   `json:"tenant_id"`
	Email       string   `json:"email,omitempty"`
	ClientType  string   `json:"client_type"` // "m2m" | "human"
	Permissions []string `json:"permissions"`
	Roles       []string `json:"roles,omitempty"`
	ExpiresAt   int64    `json:"expires_at,omitempty"`
}

type CreateTenantRequest struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type CreateAPIKeyRequest struct {
	TenantID      string   `json:"tenant_id"`
	Name          string   `json:"name"`
	Permissions   []string `json:"permissions"`
	RateLimitRPM  int      `json:"rate_limit_rpm,omitempty"`
	ExpiresInDays int      `json:"expires_in_days,omitempty"`
}

type CreateUserRequest struct {
	TenantID    string   `json:"tenant_id"`
	Email       string   `json:"email"`
	Password    string   `json:"password"`
	Roles       []string `json:"roles,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token     string       `json:"token"`
	TokenType string       `json:"token_type"`
	ExpiresIn int64        `json:"expires_in"`
	User      UserResponse `json:"user"`
}

type IssueTokenRequest struct {
	APIKey string `json:"api_key,omitempty"` // Para fluxo M2M
	Email  string `json:"email,omitempty"`   // Para fluxo de usuário
	Pass   string `json:"password,omitempty"`
}
