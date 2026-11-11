-- 001_init.sql: Schema inicial do Auth Service Multi-Tenant Central (Go Edition)

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- 1. Tenants (Clientes, Ambientes, Projetos)
CREATE TABLE IF NOT EXISTS auth_tenants (
    id VARCHAR(64) PRIMARY KEY, -- ex: 'admin', 'cliente_alpha', 'winthor', 'gestor'
    name VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active', -- 'active' | 'suspended'
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- 2. API Keys M2M de Alta Entropia (eo_live_...)
CREATE TABLE IF NOT EXISTS auth_api_keys (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL REFERENCES auth_tenants(id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    key_prefix VARCHAR(32) NOT NULL,
    key_hash VARCHAR(64) UNIQUE NOT NULL, -- SHA-256 da chave crua
    permissions TEXT[] NOT NULL DEFAULT '{"*"}', -- scopes: 'janus:*', 'vpsql:query', '*'
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    rate_limit_rpm INT NOT NULL DEFAULT 120,
    total_requests BIGINT NOT NULL DEFAULT 0,
    last_used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- 3. Usuários Multi-Tenant (Login com Senha ou OAuth)
CREATE TABLE IF NOT EXISTS auth_users (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL REFERENCES auth_tenants(id) ON DELETE CASCADE,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    roles TEXT[] NOT NULL DEFAULT '{"user"}', -- 'admin', 'operator', 'user', 'viewer'
    permissions TEXT[] NOT NULL DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- 4. Trilha de Auditoria Append-Only
CREATE TABLE IF NOT EXISTS auth_audit_log (
    id BIGSERIAL PRIMARY KEY,
    action VARCHAR(64) NOT NULL, -- 'key.created', 'key.revoked', 'user.login', 'token.issued'
    actor_id VARCHAR(64),
    tenant_id VARCHAR(64),
    ip_address VARCHAR(45),
    details JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Índices de Performance Crítica para /auth/verify
CREATE INDEX IF NOT EXISTS auth_api_keys_hash_idx ON auth_api_keys (key_hash) WHERE is_active = TRUE;
CREATE INDEX IF NOT EXISTS auth_api_keys_tenant_idx ON auth_api_keys (tenant_id);
CREATE INDEX IF NOT EXISTS auth_users_email_idx ON auth_users (lower(email));
CREATE INDEX IF NOT EXISTS auth_audit_log_tenant_idx ON auth_audit_log (tenant_id, created_at DESC);

-- Seed de Tenants Essenciais
INSERT INTO auth_tenants (id, name, status)
VALUES 
    ('admin', 'Administração Geral EliasOlie', 'active'),
    ('default', 'Workspace Padrão', 'active')
ON CONFLICT (id) DO NOTHING;
