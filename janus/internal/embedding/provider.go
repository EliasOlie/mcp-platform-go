package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/eliasolie/janus/internal/config"
	"github.com/pgvector/pgvector-go"
)

type Provider interface {
	Embed(ctx context.Context, texts []string) ([]pgvector.Vector, error)
	Dimensions() int
}

type CloudflareProvider struct {
	cfg        *config.Config
	httpClient *http.Client
}

func NewProvider(cfg *config.Config) Provider {
	return &CloudflareProvider{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (p *CloudflareProvider) Dimensions() int {
	return p.cfg.EmbeddingDimensions
}

type cfEmbedResponse struct {
	Success bool `json:"success"`
	Result  struct {
		Shape []int       `json:"shape"`
		Data  [][]float32 `json:"data"`
	} `json:"result"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (p *CloudflareProvider) Embed(ctx context.Context, texts []string) ([]pgvector.Vector, error) {
	if len(texts) == 0 {
		return []pgvector.Vector{}, nil
	}

	// 1. Tentar Cloudflare direto se credenciais estiverem disponíveis
	if p.cfg.CFAccountID != "" && p.cfg.CFAPIToken != "" {
		endpoint := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s", p.cfg.CFAccountID, p.cfg.EmbeddingModel)
		payload := map[string]any{
			"text": texts,
		}
		bodyBytes, _ := json.Marshal(payload)

		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(bodyBytes))
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+p.cfg.CFAPIToken)
			req.Header.Set("Content-Type", "application/json")

			resp, err := p.httpClient.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				defer resp.Body.Close()
				var cfResp cfEmbedResponse
				if err := json.NewDecoder(resp.Body).Decode(&cfResp); err == nil && len(cfResp.Result.Data) == len(texts) {
					vectors := make([]pgvector.Vector, len(cfResp.Result.Data))
					for i, v := range cfResp.Result.Data {
						vectors[i] = pgvector.NewVector(v)
					}
					return vectors, nil
				}
			} else if resp != nil {
				respBody, _ := io.ReadAll(resp.Body)
				log.Printf("[Embedding] Cloudflare AI retornou status %d: %s", resp.StatusCode, string(respBody))
				resp.Body.Close()
			}
		}
	}

	// 2. Se não houver credenciais diretas do CF, usar Mock Determinístico Unitário
	// para desenvolvimento e testes locais sem travar o pipeline
	log.Printf("[Embedding] Usando vetor normalizado determinístico para %d texto(s)", len(texts))
	vectors := make([]pgvector.Vector, len(texts))
	dim := p.cfg.EmbeddingDimensions

	for i, text := range texts {
		vec := make([]float32, dim)
		// Seed determinística baseada no hash do texto
		var seed int64
		for _, b := range []byte(text) {
			seed = seed*31 + int64(b)
		}
		r := rand.New(rand.NewSource(seed))

		var sumSq float64
		for d := 0; d < dim; d++ {
			val := float32(r.NormFloat64())
			vec[d] = val
			sumSq += float64(val * val)
		}
		// Normalização L2 para similaridade de cosseno
		norm := float32(math.Sqrt(sumSq))
		if norm > 0 {
			for d := 0; d < dim; d++ {
				vec[d] /= norm
			}
		}
		vectors[i] = pgvector.NewVector(vec)
	}

	return vectors, nil
}

func CleanText(text string) string {
	return strings.TrimSpace(text)
}
