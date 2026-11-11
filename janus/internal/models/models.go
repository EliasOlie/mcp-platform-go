package models

import (
	"time"

	"github.com/pgvector/pgvector-go"
)

type Decision struct {
	ID               int64           `json:"id"`
	Texto            string          `json:"texto"`
	Autor            string          `json:"autor"`
	GrupoOrigem      *string         `json:"grupo_origem,omitempty"`
	WppMessageID     *string         `json:"wpp_message_id,omitempty"`
	WppMessageURL    *string         `json:"wpp_message_url,omitempty"`
	Tema             []string        `json:"tema,omitempty"`
	Status           string          `json:"status"`
	SubstituidaPorID *int64          `json:"substituida_por_id,omitempty"`
	Embedding        pgvector.Vector `json:"-"`
	CreatedAt        time.Time       `json:"created_at"`
}

type Rule struct {
	ID         int64           `json:"id"`
	Nome       string          `json:"nome"`
	Descricao  string          `json:"descricao"`
	DecisionID *int64          `json:"decision_id,omitempty"`
	Embedding  pgvector.Vector `json:"-"`
	CreatedAt  time.Time       `json:"created_at"`
}

type OriginMessage struct {
	MessageID string `json:"message_id"`
	Autor     string `json:"autor"`
	Texto     string `json:"texto"`
	Timestamp string `json:"timestamp"`
}

type DecisionCandidate struct {
	ID              int64           `json:"id"`
	TextoExtraido   string          `json:"texto_extraido"`
	MensagensOrigem []OriginMessage `json:"mensagens_origem"`
	GrupoOrigem     *string         `json:"grupo_origem,omitempty"`
	ConfidenceScore *float64        `json:"confidence_score,omitempty"`
	Status          string          `json:"status"`
	RevisadoPor     *string         `json:"revisado_por,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

type KnowledgeDomain struct {
	ID                int64      `json:"id"`
	Slug              string     `json:"slug"`
	Nome              string     `json:"nome"`
	Descricao         *string    `json:"descricao,omitempty"`
	ItensAtivos       int        `json:"itens_ativos,omitempty"`
	ItensSubstituidos int        `json:"itens_substituidos,omitempty"`
	UltimoItemEm      *time.Time `json:"ultimo_item_em,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

type KnowledgeItem struct {
	ID               int64           `json:"id"`
	Sistema          string          `json:"sistema"`
	Tipo             string          `json:"tipo"`
	Titulo           string          `json:"titulo"`
	Conteudo         string          `json:"conteudo"`
	Descricao        string          `json:"descricao"`
	RuleIDs          []int64         `json:"rule_ids,omitempty"`
	RelacionadosIDs  []int64         `json:"relacionados_ids,omitempty"`
	Procedencia      *string         `json:"procedencia,omitempty"`
	SnapshotEm       *string         `json:"snapshot_em,omitempty"`
	ReverificarApos  *string         `json:"reverificar_apos,omitempty"`
	Status           string          `json:"status"`
	SubstituidoPorID *int64          `json:"substituido_por_id,omitempty"`
	Embedding        pgvector.Vector `json:"-"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type KnowledgeItemResumo struct {
	ID        int64     `json:"id"`
	Sistema   string    `json:"sistema"`
	Tipo      string    `json:"tipo"`
	Titulo    string    `json:"titulo"`
	Descricao string    `json:"descricao"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type KnowledgeItemWithLinks struct {
	Item              KnowledgeItem          `json:"item"`
	RegrasVinculadas  []Rule                 `json:"regras_vinculadas"`
	ItensRelacionados []KnowledgeItemResumo  `json:"itens_relacionados"`
	Historico         []KnowledgeItemResumo  `json:"historico,omitempty"`
	Score             *float64               `json:"score,omitempty"`
	Confidence        *string                `json:"confidence,omitempty"`
}

type GetKnowledgeInput struct {
	ID               *int64  `json:"id,omitempty"`
	Titulo           *string `json:"titulo,omitempty"`
	BuscaSemantica   *string `json:"busca_semantica,omitempty"`
	Sistema          *string `json:"sistema,omitempty"`
	Tipo             *string `json:"tipo,omitempty"`
	Procedencia      *string `json:"procedencia,omitempty"`
	Status           *string `json:"status,omitempty"`
	IncluirHistorico bool    `json:"incluir_historico,omitempty"`
}

type MergeDomainsResult struct {
	Origem       string  `json:"origem"`
	Destino      string  `json:"destino"`
	ItensMovidos []int64 `json:"itens_movidos"`
}

type KnowledgeAuditLog struct {
	ID         int64          `json:"id"`
	Entidade   string         `json:"entidade"`
	EntidadeID string         `json:"entidade_id"`
	Acao       string         `json:"acao"`
	Actor      *string        `json:"actor,omitempty"`
	Detalhes   map[string]any `json:"detalhes,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

type SearchHit struct {
	Source     string         `json:"source"` // "decision" | "rule" | "knowledge_item"
	Score      float64        `json:"score"`
	Confidence string         `json:"confidence"` // "alta" | "baixa"
	Item       any            `json:"item"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type SaveKnowledgeRequest struct {
	Sistema         string   `json:"sistema"`
	Tipo            string   `json:"tipo"`
	Titulo          string   `json:"titulo"`
	Conteudo        string   `json:"conteudo"`
	Descricao       string   `json:"descricao"`
	RuleIDs         []int64  `json:"rule_ids,omitempty"`
	RelacionadosIDs []int64  `json:"relacionados_ids,omitempty"`
	Procedencia     *string  `json:"procedencia,omitempty"`
	SnapshotEm      *string  `json:"snapshot_em,omitempty"`
	ReverificarApos *string  `json:"reverificar_apos,omitempty"`
	Actor           string   `json:"actor,omitempty"`
}

type RecordDecisionRequest struct {
	Texto         string   `json:"texto"`
	Autor         string   `json:"autor"`
	Grupo         string   `json:"grupo"`
	WppMessageID  string   `json:"wpp_message_id"`
	WppMessageURL *string  `json:"wpp_message_url,omitempty"`
	Tema          []string `json:"tema,omitempty"`
}

type IngestCandidateRequest struct {
	TextoExtraido   string          `json:"texto_extraido"`
	MensagensOrigem []OriginMessage `json:"mensagens_origem"`
	GrupoOrigem     *string         `json:"grupo_origem,omitempty"`
	ConfidenceScore *float64        `json:"confidence_score,omitempty"`
}
