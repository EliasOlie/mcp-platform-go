package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/eliasolie/janus/internal/database"
	"github.com/eliasolie/janus/internal/embedding"
	"github.com/eliasolie/janus/internal/models"
	"github.com/jackc/pgx/v5"
)

type Service struct {
	db       *database.DB
	embedder embedding.Provider
}

func NewService(db *database.DB, embedder embedding.Provider) *Service {
	return &Service{
		db:       db,
		embedder: embedder,
	}
}

var nonSlugRegex = regexp.MustCompile(`[^a-z0-9_-]+`)

func NormalizeSlug(input string) string {
	lower := strings.ToLower(strings.TrimSpace(input))
	slug := nonSlugRegex.ReplaceAllString(lower, "_")
	return strings.Trim(slug, "_-")
}

// EnsureDomain garante que o domínio existe em knowledge_domains
func (s *Service) EnsureDomain(ctx context.Context, rawSlug string, tx pgx.Tx) (*models.KnowledgeDomain, error) {
	slug := NormalizeSlug(rawSlug)
	if slug == "" {
		slug = "geral"
	}

	nome := strings.ToUpper(slug[:1]) + slug[1:]

	query := `
		INSERT INTO knowledge_domains (slug, nome)
		VALUES ($1, $2)
		ON CONFLICT (slug) DO UPDATE SET nome = EXCLUDED.nome
		RETURNING id, slug, nome, descricao, created_at
	`
	var d models.KnowledgeDomain
	err := tx.QueryRow(ctx, query, slug, nome).Scan(&d.ID, &d.Slug, &d.Nome, &d.Descricao, &d.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("falha ao garantir domínio: %w", err)
	}
	return &d, nil
}

// SaveKnowledge salva um novo item de conhecimento (ou Artigo de Wiki) com auto-registro de domínio e auditoria
func (s *Service) SaveKnowledge(ctx context.Context, req models.SaveKnowledgeRequest) (*models.KnowledgeItem, *models.KnowledgeDomain, error) {
	if strings.TrimSpace(req.Titulo) == "" {
		return nil, nil, fmt.Errorf("o campo 'titulo' é obrigatório")
	}
	if strings.TrimSpace(req.Conteudo) == "" {
		return nil, nil, fmt.Errorf("o campo 'conteudo' é obrigatório")
	}

	slug := NormalizeSlug(req.Sistema)
	if slug == "" {
		slug = "geral"
	}

	// Gera embedding (título + descrição + conteúdo).
	// Se for tipo "artigo" (Wiki narrativa), usa apenas título + descrição para não diluir o vetor!
	embedText := fmt.Sprintf("%s\n\n%s\n\n%s", req.Titulo, req.Descricao, req.Conteudo)
	if req.Tipo == "artigo" {
		embedText = fmt.Sprintf("%s\n\n%s", req.Titulo, req.Descricao)
	}

	vecs, err := s.embedder.Embed(ctx, []string{embedText})
	if err != nil || len(vecs) == 0 {
		return nil, nil, fmt.Errorf("falha ao gerar embedding para knowledge item: %w", err)
	}
	vec := vecs[0]

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	domain, err := s.EnsureDomain(ctx, slug, tx)
	if err != nil {
		return nil, nil, err
	}

	var embeddingVal any = vec
	if !s.db.HasVectorExtension(ctx) {
		embeddingVal = vec.Slice()
	}

	itemQuery := `
		INSERT INTO knowledge_items (
			sistema, tipo, titulo, conteudo, descricao,
			rule_ids, relacionados_ids, procedencia, snapshot_em, reverificar_apos,
			status, embedding
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'ativo', $11)
		RETURNING id, sistema, tipo, titulo, conteudo, descricao, rule_ids, relacionados_ids, procedencia, snapshot_em, reverificar_apos, status, substituido_por_id, created_at, updated_at
	`
	var item models.KnowledgeItem
	err = tx.QueryRow(ctx, itemQuery,
		domain.Slug, req.Tipo, req.Titulo, req.Conteudo, req.Descricao,
		req.RuleIDs, req.RelacionadosIDs, req.Procedencia, req.SnapshotEm, req.ReverificarApos,
		embeddingVal,
	).Scan(
		&item.ID, &item.Sistema, &item.Tipo, &item.Titulo, &item.Conteudo, &item.Descricao,
		&item.RuleIDs, &item.RelacionadosIDs, &item.Procedencia, &item.SnapshotEm, &item.ReverificarApos,
		&item.Status, &item.SubstituidoPorID, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("falha ao salvar item de conhecimento: %w", err)
	}

	// Trilha de auditoria
	actor := req.Actor
	if actor == "" {
		actor = "agy-user"
	}
	detalhesBytes, _ := json.Marshal(map[string]any{
		"sistema": item.Sistema,
		"tipo":    item.Tipo,
		"titulo":  item.Titulo,
	})

	auditQuery := `
		INSERT INTO knowledge_audit_log (entidade, entidade_id, acao, actor, detalhes)
		VALUES ('knowledge_item', $1, 'created', $2, $3)
	`
	if _, err := tx.Exec(ctx, auditQuery, fmt.Sprintf("%d", item.ID), actor, detalhesBytes); err != nil {
		return nil, nil, fmt.Errorf("falha ao gravar auditoria: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}

	return &item, domain, nil
}

// GetKnowledge busca um item ou artigo com todos os seus vínculos de regras, itens relacionados e cadeia de versões
func (s *Service) GetKnowledge(ctx context.Context, input models.GetKnowledgeInput) ([]models.KnowledgeItemWithLinks, error) {
	var items []models.KnowledgeItem

	if input.ID != nil {
		// Por ID devolve até mesmo se estiver substituído (permite auditar versões antigas)
		query := `
			SELECT id, sistema, tipo, titulo, conteudo, descricao, rule_ids, relacionados_ids,
			       procedencia, snapshot_em, reverificar_apos, status, substituido_por_id, created_at, updated_at
			FROM knowledge_items
			WHERE id = $1
		`
		var it models.KnowledgeItem
		err := s.db.Pool.QueryRow(ctx, query, *input.ID).Scan(
			&it.ID, &it.Sistema, &it.Tipo, &it.Titulo, &it.Conteudo, &it.Descricao,
			&it.RuleIDs, &it.RelacionadosIDs, &it.Procedencia, &it.SnapshotEm, &it.ReverificarApos,
			&it.Status, &it.SubstituidoPorID, &it.CreatedAt, &it.UpdatedAt,
		)
		if err == nil {
			items = append(items, it)
		}
	} else if input.Titulo != nil && *input.Titulo != "" {
		query := `
			SELECT id, sistema, tipo, titulo, conteudo, descricao, rule_ids, relacionados_ids,
			       procedencia, snapshot_em, reverificar_apos, status, substituido_por_id, created_at, updated_at
			FROM knowledge_items
			WHERE lower(titulo) LIKE lower($1) AND status = 'ativo'
			LIMIT 10
		`
		rows, err := s.db.Pool.Query(ctx, query, "%"+*input.Titulo+"%")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var it models.KnowledgeItem
				if err := rows.Scan(
					&it.ID, &it.Sistema, &it.Tipo, &it.Titulo, &it.Conteudo, &it.Descricao,
					&it.RuleIDs, &it.RelacionadosIDs, &it.Procedencia, &it.SnapshotEm, &it.ReverificarApos,
					&it.Status, &it.SubstituidoPorID, &it.CreatedAt, &it.UpdatedAt,
				); err == nil {
					items = append(items, it)
				}
			}
		}
	}

	var results []models.KnowledgeItemWithLinks
	for _, it := range items {
		regras, _ := s.getRulesByIds(ctx, it.RuleIDs)
		relacionados, _ := s.getItemResumosByIds(ctx, it.RelacionadosIDs)

		var historico []models.KnowledgeItemResumo
		if input.IncluirHistorico {
			historico, _ = s.getSupersedeChain(ctx, it.ID)
		}

		results = append(results, models.KnowledgeItemWithLinks{
			Item:              it,
			RegrasVinculadas:  regras,
			ItensRelacionados: relacionados,
			Historico:         historico,
		})
	}

	return results, nil
}

// ListKnowledge lista itens paginados com suporte a filtros
func (s *Service) ListKnowledge(ctx context.Context, sistema, tipo, status string, limit, offset int) ([]models.KnowledgeItemResumo, error) {
	if limit <= 0 {
		limit = 20
	}
	if status == "" {
		status = "ativo"
	}

	query := `
		SELECT id, sistema, tipo, titulo, descricao, status, created_at
		FROM knowledge_items
		WHERE ($1 = '' OR lower(sistema) = lower($1))
		  AND ($2 = '' OR tipo = $2)
		  AND ($3 = 'todos' OR status = $3)
		ORDER BY created_at DESC
		LIMIT $4 OFFSET $5
	`
	rows, err := s.db.Pool.Query(ctx, query, sistema, tipo, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.KnowledgeItemResumo
	for rows.Next() {
		var r models.KnowledgeItemResumo
		if err := rows.Scan(&r.ID, &r.Sistema, &r.Tipo, &r.Titulo, &r.Descricao, &r.Status, &r.CreatedAt); err == nil {
			list = append(list, r)
		}
	}
	return list, nil
}

// SupersedeKnowledge substitui uma versão antiga de forma não destrutiva por um novo item
func (s *Service) SupersedeKnowledge(ctx context.Context, idAntigo int64, novoReq models.SaveKnowledgeRequest, actor string) (*models.KnowledgeItem, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// 1. Cria o novo item
	novoItem, _, err := s.SaveKnowledge(ctx, novoReq)
	if err != nil {
		return nil, fmt.Errorf("falha ao criar novo item para supersede: %w", err)
	}

	// 2. Marca o antigo como substituído apontando para o novo
	updateQuery := `
		UPDATE knowledge_items
		SET status = 'substituido', substituido_por_id = $1, updated_at = NOW()
		WHERE id = $2
	`
	if _, err := tx.Exec(ctx, updateQuery, novoItem.ID, idAntigo); err != nil {
		return nil, fmt.Errorf("falha ao atualizar status do item antigo: %w", err)
	}

	// 3. Registra auditoria
	if actor == "" {
		actor = "agy-user"
	}
	detalhesBytes, _ := json.Marshal(map[string]any{
		"substituido_por_id": novoItem.ID,
		"item_antigo_id":     idAntigo,
	})
	auditQuery := `
		INSERT INTO knowledge_audit_log (entidade, entidade_id, acao, actor, detalhes)
		VALUES ('knowledge_item', $1, 'superseded', $2, $3)
	`
	_, _ = tx.Exec(ctx, auditQuery, fmt.Sprintf("%d", idAntigo), actor, detalhesBytes)

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return novoItem, nil
}

// DeleteKnowledge executa soft-delete/remoção com snapshot salvo integralmente na auditoria
func (s *Service) DeleteKnowledge(ctx context.Context, id int64, actor string) error {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var it models.KnowledgeItem
	query := `
		SELECT id, sistema, tipo, titulo, conteudo, descricao
		FROM knowledge_items
		WHERE id = $1
	`
	if err := tx.QueryRow(ctx, query, id).Scan(&it.ID, &it.Sistema, &it.Tipo, &it.Titulo, &it.Conteudo, &it.Descricao); err != nil {
		return fmt.Errorf("item não encontrado: %w", err)
	}

	deleteQuery := `DELETE FROM knowledge_items WHERE id = $1`
	if _, err := tx.Exec(ctx, deleteQuery, id); err != nil {
		return fmt.Errorf("falha ao deletar item: %w", err)
	}

	if actor == "" {
		actor = "agy-user"
	}
	snapshotBytes, _ := json.Marshal(it)
	auditQuery := `
		INSERT INTO knowledge_audit_log (entidade, entidade_id, acao, actor, detalhes)
		VALUES ('knowledge_item', $1, 'deleted', $2, $3)
	`
	_, _ = tx.Exec(ctx, auditQuery, fmt.Sprintf("%d", id), actor, snapshotBytes)

	return tx.Commit(ctx)
}

// SupersedeDecision marca uma decisão antiga como substituída por uma nova
func (s *Service) SupersedeDecision(ctx context.Context, antigoID, novoID int64) error {
	query := `
		UPDATE decisions
		SET status = 'substituida', substituida_por_id = $1
		WHERE id = $2
	`
	_, err := s.db.Pool.Exec(ctx, query, novoID, antigoID)
	return err
}

// MergeDomains unifica um domínio em outro e move todos os itens de conhecimento
func (s *Service) MergeDomains(ctx context.Context, origem, destinoRaw, actor string) (*models.MergeDomainsResult, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	destinoSlug := NormalizeSlug(destinoRaw)
	if destinoSlug == "" {
		return nil, fmt.Errorf("slug de destino inválido")
	}

	destino, err := s.EnsureDomain(ctx, destinoSlug, tx)
	if err != nil {
		return nil, err
	}

	// Move todos os itens do domínio origem para o destino
	moveQuery := `
		UPDATE knowledge_items
		SET sistema = $1, updated_at = NOW()
		WHERE lower(sistema) = lower($2)
		RETURNING id
	`
	rows, err := tx.Query(ctx, moveQuery, destino.Slug, origem)
	if err != nil {
		return nil, fmt.Errorf("falha ao mover itens: %w", err)
	}
	defer rows.Close()

	var itensMovidos []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			itensMovidos = append(itensMovidos, id)
		}
	}

	// Deleta domínio de origem se existir na tabela
	_, _ = tx.Exec(ctx, `DELETE FROM knowledge_domains WHERE lower(slug) = lower($1)`, origem)

	// Grava auditoria
	if actor == "" {
		actor = "agy-user"
	}
	detalhes, _ := json.Marshal(map[string]any{
		"origem":        origem,
		"destino":       destino.Slug,
		"itens_movidos": itensMovidos,
	})
	_, _ = tx.Exec(ctx, `
		INSERT INTO knowledge_audit_log (entidade, entidade_id, acao, actor, detalhes)
		VALUES ('knowledge_domain', $1, 'domain_merged', $2, $3)
	`, origem, actor, detalhes)

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &models.MergeDomainsResult{
		Origem:       origem,
		Destino:      destino.Slug,
		ItensMovidos: itensMovidos,
	}, nil
}

// GetAuditLog consulta as últimas entradas da trilha de auditoria
func (s *Service) GetAuditLog(ctx context.Context, entidade, entidadeID string, limit int) ([]models.KnowledgeAuditLog, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT id, entidade, entidade_id, acao, actor, detalhes, created_at
		FROM knowledge_audit_log
		WHERE ($1 = '' OR entidade = $1)
		  AND ($2 = '' OR entidade_id = $2)
		ORDER BY created_at DESC
		LIMIT $3
	`
	rows, err := s.db.Pool.Query(ctx, query, entidade, entidadeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.KnowledgeAuditLog
	for rows.Next() {
		var l models.KnowledgeAuditLog
		var rawDetalhes []byte
		if err := rows.Scan(&l.ID, &l.Entidade, &l.EntidadeID, &l.Acao, &l.Actor, &rawDetalhes, &l.CreatedAt); err == nil {
			if len(rawDetalhes) > 0 {
				_ = json.Unmarshal(rawDetalhes, &l.Detalhes)
			}
			logs = append(logs, l)
		}
	}
	return logs, nil
}

// RecordDecision grava uma decisão literal originada de conversa ou registro
func (s *Service) RecordDecision(ctx context.Context, req models.RecordDecisionRequest) (*models.Decision, error) {
	if strings.TrimSpace(req.Texto) == "" {
		return nil, fmt.Errorf("o campo 'texto' é obrigatório")
	}
	if strings.TrimSpace(req.Autor) == "" {
		return nil, fmt.Errorf("o campo 'autor' é obrigatório")
	}

	vecs, err := s.embedder.Embed(ctx, []string{req.Texto})
	if err != nil || len(vecs) == 0 {
		return nil, fmt.Errorf("falha ao gerar embedding para decisão: %w", err)
	}

	var embeddingVal any = vecs[0]
	if !s.db.HasVectorExtension(ctx) {
		embeddingVal = vecs[0].Slice()
	}

	query := `
		INSERT INTO decisions (texto, autor, grupo_origem, wpp_message_id, wpp_message_url, tema, status, embedding)
		VALUES ($1, $2, $3, $4, $5, $6, 'vigente', $7)
		RETURNING id, texto, autor, grupo_origem, wpp_message_id, wpp_message_url, tema, status, substituida_por_id, created_at
	`
	var d models.Decision
	err = s.db.Pool.QueryRow(ctx, query,
		req.Texto, req.Autor, req.Grupo, req.WppMessageID, req.WppMessageURL, req.Tema, embeddingVal,
	).Scan(
		&d.ID, &d.Texto, &d.Autor, &d.GrupoOrigem, &d.WppMessageID, &d.WppMessageURL,
		&d.Tema, &d.Status, &d.SubstituidaPorID, &d.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("falha ao gravar decisão: %w", err)
	}

	return &d, nil
}

// IngestCandidate registra uma nova extração para fila de aprovação humana
func (s *Service) IngestCandidate(ctx context.Context, req models.IngestCandidateRequest) (*models.DecisionCandidate, error) {
	if strings.TrimSpace(req.TextoExtraido) == "" {
		return nil, fmt.Errorf("texto_extraido é obrigatório")
	}

	mensagensJSON, err := json.Marshal(req.MensagensOrigem)
	if err != nil {
		return nil, fmt.Errorf("erro ao serializar mensagens_origem: %w", err)
	}

	query := `
		INSERT INTO decision_candidates (texto_extraido, mensagens_origem, grupo_origem, confidence_score, status)
		VALUES ($1, $2, $3, $4, 'pendente')
		RETURNING id, texto_extraido, grupo_origem, confidence_score, status, revisado_por, created_at
	`
	var c models.DecisionCandidate
	err = s.db.Pool.QueryRow(ctx, query, req.TextoExtraido, mensagensJSON, req.GrupoOrigem, req.ConfidenceScore).
		Scan(&c.ID, &c.TextoExtraido, &c.GrupoOrigem, &c.ConfidenceScore, &c.Status, &c.RevisadoPor, &c.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("falha ao inserir candidato: %w", err)
	}
	c.MensagensOrigem = req.MensagensOrigem
	return &c, nil
}

// ListDomains retorna todos os domínios com contagem de itens
func (s *Service) ListDomains(ctx context.Context) ([]models.KnowledgeDomain, error) {
	query := `
		SELECT d.id, d.slug, d.nome, d.descricao, d.created_at,
		       count(k.id) FILTER (WHERE k.status = 'ativo')::int AS itens_ativos,
		       count(k.id) FILTER (WHERE k.status = 'substituido')::int AS itens_substituidos,
		       max(k.created_at) AS ultimo_item_em
		FROM knowledge_domains d
		LEFT JOIN knowledge_items k ON k.sistema = d.slug
		GROUP BY d.id
		ORDER BY d.nome, d.slug
	`
	rows, err := s.db.Pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var domains []models.KnowledgeDomain
	for rows.Next() {
		var d models.KnowledgeDomain
		if err := rows.Scan(&d.ID, &d.Slug, &d.Nome, &d.Descricao, &d.CreatedAt, &d.ItensAtivos, &d.ItensSubstituidos, &d.UltimoItemEm); err != nil {
			continue
		}
		domains = append(domains, d)
	}
	return domains, nil
}

// Auxiliares para montar relacionamentos
func (s *Service) getRulesByIds(ctx context.Context, ids []int64) ([]models.Rule, error) {
	if len(ids) == 0 {
		return []models.Rule{}, nil
	}
	query := `SELECT id, nome, descricao, decision_id, created_at FROM rules WHERE id = ANY($1)`
	rows, err := s.db.Pool.Query(ctx, query, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []models.Rule
	for rows.Next() {
		var r models.Rule
		if err := rows.Scan(&r.ID, &r.Nome, &r.Descricao, &r.DecisionID, &r.CreatedAt); err == nil {
			rules = append(rules, r)
		}
	}
	return rules, nil
}

func (s *Service) getItemResumosByIds(ctx context.Context, ids []int64) ([]models.KnowledgeItemResumo, error) {
	if len(ids) == 0 {
		return []models.KnowledgeItemResumo{}, nil
	}
	query := `SELECT id, sistema, tipo, titulo, descricao, status, created_at FROM knowledge_items WHERE id = ANY($1)`
	rows, err := s.db.Pool.Query(ctx, query, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.KnowledgeItemResumo
	for rows.Next() {
		var r models.KnowledgeItemResumo
		if err := rows.Scan(&r.ID, &r.Sistema, &r.Tipo, &r.Titulo, &r.Descricao, &r.Status, &r.CreatedAt); err == nil {
			list = append(list, r)
		}
	}
	return list, nil
}

func (s *Service) getSupersedeChain(ctx context.Context, startID int64) ([]models.KnowledgeItemResumo, error) {
	query := `
		WITH RECURSIVE chain AS (
			SELECT id, sistema, tipo, titulo, descricao, status, substituido_por_id, created_at
			FROM knowledge_items WHERE id = $1
			UNION ALL
			SELECT k.id, k.sistema, k.tipo, k.titulo, k.descricao, k.status, k.substituido_por_id, k.created_at
			FROM knowledge_items k
			INNER JOIN chain c ON k.id = c.substituido_por_id
		)
		SELECT id, sistema, tipo, titulo, descricao, status, created_at
		FROM chain
		ORDER BY created_at ASC
	`
	rows, err := s.db.Pool.Query(ctx, query, startID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chain []models.KnowledgeItemResumo
	for rows.Next() {
		var r models.KnowledgeItemResumo
		if err := rows.Scan(&r.ID, &r.Sistema, &r.Tipo, &r.Titulo, &r.Descricao, &r.Status, &r.CreatedAt); err == nil {
			chain = append(chain, r)
		}
	}
	return chain, nil
}
