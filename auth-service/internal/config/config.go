package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port                   int
	DatabaseURL            string
	AdminMasterKey         string
	Issuer                 string
	Audience               string
	VPSQLURL               string
	BootstrapAdminEmail    string
	BootstrapAdminPassword string
}

func Load() *Config {
	port := 8081
	if p, err := strconv.Atoi(os.Getenv("PORT")); err == nil && p > 0 {
		port = p
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgresql://app_janus_local_admin:CHANGE_ME@db.example.com:6432/app_janus_local?sslmode=disable"
	}

	masterKey := os.Getenv("ADMIN_MASTER_KEY")
	if masterKey == "" {
		masterKey = "eo_master_secret_key_change_me_in_prod"
	}

	issuer := os.Getenv("AUTH_ISSUER")
	if issuer == "" {
		issuer = "https://auth.example.com"
	}

	audience := os.Getenv("AUTH_AUDIENCE")
	if audience == "" {
		audience = "eliasolie-platform"
	}

	vpsqlURL := os.Getenv("VPSQL_URL")
	if vpsqlURL == "" {
		vpsqlURL = "https://vpsql.example.com"
	}

	bootstrapEmail := os.Getenv("BOOTSTRAP_ADMIN_EMAIL")
	if bootstrapEmail == "" {
		bootstrapEmail = "admin@example.com"
	}

	bootstrapPass := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if bootstrapPass == "" {
		bootstrapPass = ""
	}

	return &Config{
		Port:                   port,
		DatabaseURL:            dbURL,
		AdminMasterKey:         masterKey,
		Issuer:                 issuer,
		Audience:               audience,
		VPSQLURL:               vpsqlURL,
		BootstrapAdminEmail:    bootstrapEmail,
		BootstrapAdminPassword: bootstrapPass,
	}
}
