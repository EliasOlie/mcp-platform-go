-- 001_init.sql: Schema resiliente do Janus Core (Cérebro Operacional)
-- Suporte automático a pgvector quando disponível, com fallback gracioso

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'vector') THEN
        CREATE EXTENSION IF NOT EXISTS "vector";
    END IF;
END $$;

-- 1. Domínios de conhecimento dinâmicos (Clientes, Projetos, etc)
CREATE TABLE IF NOT EXISTS knowledge_domains (
    id BIGSERIAL PRIMARY KEY,
    slug TEXT UNIQUE NOT NULL,
    nome TEXT NOT NULL,
    descricao TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- 2. Decisões literais auditáveis
CREATE TABLE IF NOT EXISTS decisions (
    id BIGSERIAL PRIMARY KEY,
    texto TEXT NOT NULL,
    autor TEXT NOT NULL,
    grupo_origem TEXT,
    wpp_message_id TEXT,
    wpp_message_url TEXT,
    tema TEXT[],
    status TEXT DEFAULT 'vigente', -- 'vigente' | 'substituida'
    substituida_por_id BIGINT REFERENCES decisions(id),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- 3. Regras de negócio derivadas
CREATE TABLE IF NOT EXISTS rules (
    id BIGSERIAL PRIMARY KEY,
    nome TEXT NOT NULL,
    descricao TEXT NOT NULL,
    decision_id BIGINT REFERENCES decisions(id),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- 4. Fila de revisão de candidatos (WhatsApp / extrações)
CREATE TABLE IF NOT EXISTS decision_candidates (
    id BIGSERIAL PRIMARY KEY,
    texto_extraido TEXT NOT NULL,
    mensagens_origem JSONB NOT NULL,
    grupo_origem TEXT,
    confidence_score FLOAT,
    status TEXT DEFAULT 'pendente', -- 'pendente' | 'aprovado' | 'reprovado' | 'escalado'
    revisado_por TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- 5. Itens de conhecimento multi-sistema e WIKIS de Projetos/Clientes
CREATE TABLE IF NOT EXISTS knowledge_items (
    id BIGSERIAL PRIMARY KEY,
    sistema TEXT NOT NULL,
    tipo TEXT NOT NULL, -- 'artigo' (Wiki), 'query_sql', 'regra', 'procedimento', 'faq'
    titulo TEXT NOT NULL,
    conteudo TEXT NOT NULL,
    descricao TEXT NOT NULL,
    rule_ids BIGINT[],
    relacionados_ids BIGINT[],
    procedencia TEXT CHECK (procedencia IN ('verificado_query', 'confirmado_pessoa', 'hipotese')),
    snapshot_em DATE,
    reverificar_apos DATE,
    status TEXT DEFAULT 'ativo', -- 'ativo' | 'substituido'
    substituido_por_id BIGINT REFERENCES knowledge_items(id),
    tsv_content TSVECTOR GENERATED ALWAYS AS (
        to_tsvector('portuguese', coalesce(titulo, '') || ' ' || coalesce(descricao, '') || ' ' || coalesce(conteudo, ''))
    ) STORED,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- 6. Trilha de auditoria append-only
CREATE TABLE IF NOT EXISTS knowledge_audit_log (
    id BIGSERIAL PRIMARY KEY,
    entidade TEXT NOT NULL,
    entidade_id TEXT NOT NULL,
    acao TEXT NOT NULL,
    actor TEXT,
    detalhes JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Adiciona coluna de embedding dinamicamente conforme disponibilidade da extensão vector
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector') THEN
        ALTER TABLE decisions ADD COLUMN IF NOT EXISTS embedding VECTOR(1024);
        ALTER TABLE rules ADD COLUMN IF NOT EXISTS embedding VECTOR(1024);
        ALTER TABLE knowledge_items ADD COLUMN IF NOT EXISTS embedding VECTOR(1024);
        
        CREATE INDEX IF NOT EXISTS decisions_embedding_hnsw ON decisions USING hnsw (embedding vector_cosine_ops);
        CREATE INDEX IF NOT EXISTS rules_embedding_hnsw ON rules USING hnsw (embedding vector_cosine_ops);
        CREATE INDEX IF NOT EXISTS knowledge_items_embedding_hnsw ON knowledge_items USING hnsw (embedding vector_cosine_ops);
    ELSE
        -- Fallback seguro para armazenamento vetorial antes da compilação do pgvector
        ALTER TABLE decisions ADD COLUMN IF NOT EXISTS embedding REAL[];
        ALTER TABLE rules ADD COLUMN IF NOT EXISTS embedding REAL[];
        ALTER TABLE knowledge_items ADD COLUMN IF NOT EXISTS embedding REAL[];
    END IF;
END $$;

-- Índices Full-Text Search GIN para busca léxica de alta precisão
CREATE INDEX IF NOT EXISTS knowledge_items_tsv_idx ON knowledge_items USING gin (tsv_content);
CREATE INDEX IF NOT EXISTS knowledge_items_sistema_idx ON knowledge_items (lower(sistema), created_at DESC);
CREATE INDEX IF NOT EXISTS knowledge_audit_log_entidade_idx ON knowledge_audit_log (entidade, entidade_id);
