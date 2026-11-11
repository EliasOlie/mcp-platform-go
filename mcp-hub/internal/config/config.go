package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port           int
	AuthServiceURL string
	VPSQLURL       string
	AdminToken     string
	JanusURL       string
	PublicURL      string
}

func Load() *Config {
	port := 8082
	if p, err := strconv.Atoi(os.Getenv("PORT")); err == nil && p > 0 {
		port = p
	}

	authURL := os.Getenv("AUTH_SERVICE_URL")
	if authURL == "" {
		authURL = "http://localhost:8081"
	}

	vpsqlURL := os.Getenv("VPSQL_URL")
	if vpsqlURL == "" {
		vpsqlURL = "https://vpsql.example.com"
	}

	janusURL := os.Getenv("JANUS_SERVICE_URL")
	if janusURL == "" {
		janusURL = "http://localhost:3030"
	}

	pubURL := os.Getenv("PUBLIC_URL")
	if pubURL == "" {
		pubURL = "https://mcp.example.com"
	}

	return &Config{
		Port:           port,
		AuthServiceURL: authURL,
		VPSQLURL:       vpsqlURL,
		AdminToken:     os.Getenv("ADMIN_TOKEN"),
		JanusURL:       janusURL,
		PublicURL:      pubURL,
	}
}
