package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                int
	DatabaseURL         string
	APIKey              string
	VPSQLAIURL          string
	VPSQLAIKey          string
	CFAccountID         string
	CFAPIToken          string
	EmbeddingModel      string
	EmbeddingDimensions int
	SearchTopK          int
}

func Load() *Config {
	_ = godotenv.Load()

	port := 3030
	if p, err := strconv.Atoi(os.Getenv("PORT")); err == nil && p > 0 {
		port = p
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgresql://app_janus_local_admin:CHANGE_ME@db.example.com:6432/app_janus_local?sslmode=disable"
	}

	dimensions := 1024
	if d, err := strconv.Atoi(os.Getenv("EMBEDDING_DIMENSIONS")); err == nil && d > 0 {
		dimensions = d
	}

	topK := 5
	if k, err := strconv.Atoi(os.Getenv("SEARCH_TOP_K")); err == nil && k > 0 {
		topK = k
	}

	model := os.Getenv("EMBEDDING_MODEL")
	if model == "" {
		model = "@cf/baai/bge-m3"
	}

	vpsqlURL := os.Getenv("VPSQL_AI_URL")
	if vpsqlURL == "" {
		vpsqlURL = "https://vpsql.example.com"
	}

	vpsqlKey := os.Getenv("VPSQL_AI_KEY")
	if vpsqlKey == "" {
		vpsqlKey = "vpsql_sk_live_fa2b0f3f0f89954a76f967c88d7c2e47"
	}

	return &Config{
		Port:                port,
		DatabaseURL:         dbURL,
		APIKey:              os.Getenv("JANUS_API_KEY"),
		VPSQLAIURL:          vpsqlURL,
		VPSQLAIKey:          vpsqlKey,
		CFAccountID:         os.Getenv("CF_ACCOUNT_ID"),
		CFAPIToken:          os.Getenv("CF_API_TOKEN"),
		EmbeddingModel:      model,
		EmbeddingDimensions: dimensions,
		SearchTopK:          topK,
	}
}
