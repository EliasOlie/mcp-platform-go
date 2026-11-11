package search

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/eliasolie/janus/internal/database"
	"github.com/eliasolie/janus/internal/embedding"
	"github.com/eliasolie/janus/internal/models"
	"github.com/pgvector/pgvector-go"
)

type Engine struct {
	db       *database.DB
	embedder embedding.Provider
	topK     int
}

func NewEngine(db *database.DB, embedder embedding.Provider, topK int) *Engine {
	if topK <= 0 {
		topK = 5
	}
	return &Engine{
		db:       db,
		embedder: embedder,
		topK:     topK,
	}
}

type FilterOptions struct {
	Sistema     string
	Tipo        string
	Procedencia string
	Status      string
}

// Search executa RAG híbrido com Reciprocal Rank Fusion (RRF) combinando busca vetorial e léxica
func (e *Engine) Search(ctx context.Context, query string, filters FilterOptions) ([]models.SearchHit, error) {
	vecs, err := e.embedder.Embed(ctx, []string{query})
	if err != nil || len(vecs) == 0 {
		return nil, fmt.Errorf("falha ao gerar embedding para query: %w", err)
	}
	queryVec := vecs[0]

	// 1. Busca vetorial densa em knowledge_items
	denseHits, err := e.searchDenseKnowledge(ctx, queryVec, filters, e.topK*2)
	if err != nil {
		return nil, err
	}

	// 2. Busca léxica Full-Text Search em knowledge_items
	sparseHits, err := e.searchSparseKnowledge(ctx, query, filters, e.topK*2)
	if err != nil {
		// Se FTS falhar, prossegue com os resultados vetoriais
		sparseHits = []models.SearchHit{}
	}

	// 3. Busca vetorial em decisions e rules
	decisionHits, _ := e.searchDecisions(ctx, queryVec, e.topK)
	ruleHits, _ := e.searchRules(ctx, queryVec, e.topK)

	// 4. Aplica RRF (Reciprocal Rank Fusion, k=60) nos itens de conhecimento
	combined := e.applyRRF(denseHits, sparseHits, 60)

	// Junta com decisions e rules
	allResults := append(combined, decisionHits...)
	allResults = append(allResults, ruleHits...)

	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Score > allResults[j].Score
	})

	// 5. Aplica cálculo de Confiança Relativa
	withConfidence := applyConfidence(allResults)

	if len(withConfidence) > e.topK {
		return withConfidence[:e.topK], nil
	}
	return withConfidence, nil
}

func (e *Engine) searchDenseKnowledge(ctx context.Context, vec pgvector.Vector, f FilterOptions, limit int) ([]models.SearchHit, error) {
	status := f.Status
	if status == "" {
		status = "ativo"
	}

	query := `
		SELECT id, sistema, tipo, titulo, conteudo, descricao, rule_ids, relacionados_ids, procedencia, snapshot_em, reverificar_apos, status, substituido_por_id, created_at, updated_at,
		       1 - (embedding <=> $1) AS similarity
		FROM knowledge_items
		WHERE status = $2
	`
	args := []any{vec, status}
	argIdx := 3

	if f.Sistema != "" {
		query += fmt.Sprintf(" AND lower(sistema) = lower($%d)", argIdx)
		args = append(args, f.Sistema)
		argIdx++
	}
	if f.Tipo != "" {
		query += fmt.Sprintf(" AND tipo = $%d", argIdx)
		args = append(args, f.Tipo)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY embedding <=> $1 ASC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := e.db.Pool.Query(ctx, query, args...)
	if err != nil {
		// Se pgvector não estiver instalado ou operador <=> não existir, degrada graciosamente para busca léxica
		return []models.SearchHit{}, nil
	}
	defer rows.Close()

	var hits []models.SearchHit
	for rows.Next() {
		var item models.KnowledgeItem
		var sim float64
		if err := rows.Scan(
			&item.ID, &item.Sistema, &item.Tipo, &item.Titulo, &item.Conteudo, &item.Descricao,
			&item.RuleIDs, &item.RelacionadosIDs, &item.Procedencia, &item.SnapshotEm, &item.ReverificarApos,
			&item.Status, &item.SubstituidoPorID, &item.CreatedAt, &item.UpdatedAt, &sim,
		); err != nil {
			continue
		}
		hits = append(hits, models.SearchHit{
			Source: "knowledge_item",
			Score:  sim,
			Item:   item,
			Metadata: map[string]any{
				"sistema": item.Sistema,
				"tipo":    item.Tipo,
				"titulo":  item.Titulo,
			},
		})
	}
	return hits, nil
}

func (e *Engine) searchSparseKnowledge(ctx context.Context, text string, f FilterOptions, limit int) ([]models.SearchHit, error) {
	status := f.Status
	if status == "" {
		status = "ativo"
	}

	query := `
		SELECT id, sistema, tipo, titulo, conteudo, descricao, rule_ids, relacionados_ids, procedencia, snapshot_em, reverificar_apos, status, substituido_por_id, created_at, updated_at,
		       ts_rank(tsv_content, plainto_tsquery('portuguese', $1)) AS rank_score
		FROM knowledge_items
		WHERE status = $2 AND tsv_content @@ plainto_tsquery('portuguese', $1)
	`
	args := []any{text, status}
	argIdx := 3

	if f.Sistema != "" {
		query += fmt.Sprintf(" AND lower(sistema) = lower($%d)", argIdx)
		args = append(args, f.Sistema)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY rank_score DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := e.db.Pool.Query(ctx, query, args...)
	if err != nil {
		// Se pgvector não estiver instalado ou operador <=> não existir, degrada graciosamente para busca léxica
		return []models.SearchHit{}, nil
	}
	defer rows.Close()

	var hits []models.SearchHit
	for rows.Next() {
		var item models.KnowledgeItem
		var rank float64
		if err := rows.Scan(
			&item.ID, &item.Sistema, &item.Tipo, &item.Titulo, &item.Conteudo, &item.Descricao,
			&item.RuleIDs, &item.RelacionadosIDs, &item.Procedencia, &item.SnapshotEm, &item.ReverificarApos,
			&item.Status, &item.SubstituidoPorID, &item.CreatedAt, &item.UpdatedAt, &rank,
		); err != nil {
			continue
		}
		hits = append(hits, models.SearchHit{
			Source: "knowledge_item",
			Score:  rank,
			Item:   item,
			Metadata: map[string]any{
				"sistema": item.Sistema,
				"tipo":    item.Tipo,
				"titulo":  item.Titulo,
			},
		})
	}
	return hits, nil
}

func (e *Engine) searchDecisions(ctx context.Context, vec pgvector.Vector, limit int) ([]models.SearchHit, error) {
	query := `
		SELECT id, texto, autor, grupo_origem, wpp_message_id, wpp_message_url, tema, status, substituida_por_id, created_at,
		       1 - (embedding <=> $1) AS similarity
		FROM decisions
		WHERE status = 'vigente'
		ORDER BY embedding <=> $1 ASC
		LIMIT $2
	`
	rows, err := e.db.Pool.Query(ctx, query, vec, limit)
	if err != nil {
		return []models.SearchHit{}, nil
	}
	defer rows.Close()

	var hits []models.SearchHit
	for rows.Next() {
		var d models.Decision
		var sim float64
		if err := rows.Scan(
			&d.ID, &d.Texto, &d.Autor, &d.GrupoOrigem, &d.WppMessageID, &d.WppMessageURL,
			&d.Tema, &d.Status, &d.SubstituidaPorID, &d.CreatedAt, &sim,
		); err != nil {
			continue
		}
		hits = append(hits, models.SearchHit{
			Source: "decision",
			Score:  sim,
			Item:   d,
			Metadata: map[string]any{
				"autor": d.Autor,
				"grupo": d.GrupoOrigem,
			},
		})
	}
	return hits, nil
}

func (e *Engine) searchRules(ctx context.Context, vec pgvector.Vector, limit int) ([]models.SearchHit, error) {
	query := `
		SELECT id, nome, descricao, decision_id, created_at,
		       1 - (embedding <=> $1) AS similarity
		FROM rules
		ORDER BY embedding <=> $1 ASC
		LIMIT $2
	`
	rows, err := e.db.Pool.Query(ctx, query, vec, limit)
	if err != nil {
		return []models.SearchHit{}, nil
	}
	defer rows.Close()

	var hits []models.SearchHit
	for rows.Next() {
		var r models.Rule
		var sim float64
		if err := rows.Scan(&r.ID, &r.Nome, &r.Descricao, &r.DecisionID, &r.CreatedAt, &sim); err != nil {
			continue
		}
		hits = append(hits, models.SearchHit{
			Source: "rule",
			Score:  sim,
			Item:   r,
			Metadata: map[string]any{
				"nome": r.Nome,
			},
		})
	}
	return hits, nil
}

func (e *Engine) applyRRF(dense, sparse []models.SearchHit, k float64) []models.SearchHit {
	scoreMap := make(map[int64]float64)
	itemMap := make(map[int64]models.SearchHit)

	for rank, hit := range dense {
		item := hit.Item.(models.KnowledgeItem)
		scoreMap[item.ID] += 1.0 / (k + float64(rank+1))
		itemMap[item.ID] = hit
	}

	for rank, hit := range sparse {
		item := hit.Item.(models.KnowledgeItem)
		scoreMap[item.ID] += 1.0 / (k + float64(rank+1))
		if _, exists := itemMap[item.ID]; !exists {
			itemMap[item.ID] = hit
		}
	}

	var combined []models.SearchHit
	for id, rrfScore := range scoreMap {
		hit := itemMap[id]
		hit.Score = rrfScore
		combined = append(combined, hit)
	}

	sort.Slice(combined, func(i, j int) bool {
		return combined[i].Score > combined[j].Score
	})

	return combined
}

func applyConfidence(hits []models.SearchHit) []models.SearchHit {
	if len(hits) == 0 {
		return hits
	}

	for i := range hits {
		if i == 0 {
			if len(hits) == 1 || (hits[0].Score-hits[1].Score) >= 0.02 {
				hits[i].Confidence = "alta"
			} else {
				hits[i].Confidence = "baixa"
			}
		} else {
			hits[i].Confidence = "baixa"
		}
		// Arredonda score para 4 casas decimais
		hits[i].Score = math.Round(hits[i].Score*10000) / 10000
	}
	return hits
}
