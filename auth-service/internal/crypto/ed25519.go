package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type KeyManager struct {
	mu         sync.RWMutex
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	keyID      string
}

func NewKeyManager() (*KeyManager, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("falha ao gerar par de chaves Ed25519: %w", err)
	}

	h := sha256.Sum256(pub)
	keyID := hex.EncodeToString(h[:8])

	return &KeyManager{
		privateKey: priv,
		publicKey:  pub,
		keyID:      keyID,
	}, nil
}

func (km *KeyManager) KeyID() string {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.keyID
}

func (km *KeyManager) PublicKey() ed25519.PublicKey {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.publicKey
}

// JWK representa uma chave pública no padrão RFC 7517
type JWK struct {
	Kty string `json:"kty"` // "OKP" para Ed25519
	Crv string `json:"crv"` // "Ed25519"
	X   string `json:"x"`   // base64url da chave pública
	Kid string `json:"kid"`
	Use string `json:"use"` // "sig"
	Alg string `json:"alg"` // "EdDSA"
}

type JWKS struct {
	Keys []JWK `json:"keys"`
}

func (km *KeyManager) GetJWKS() JWKS {
	km.mu.RLock()
	defer km.mu.RUnlock()

	pubBase64 := base64.RawURLEncoding.EncodeToString(km.publicKey)
	return JWKS{
		Keys: []JWK{
			{
				Kty: "OKP",
				Crv: "Ed25519",
				X:   pubBase64,
				Kid: km.keyID,
				Use: "sig",
				Alg: "EdDSA",
			},
		},
	}
}

// SignToken assina um payload no formato JWT EdDSA
func (km *KeyManager) SignToken(claimsJSON []byte) (string, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	headerJSON := fmt.Sprintf(`{"alg":"EdDSA","typ":"JWT","kid":"%s"}`, km.keyID)
	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(headerJSON))
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	signingInput := fmt.Sprintf("%s.%s", headerB64, claimsB64)
	signature := ed25519.Sign(km.privateKey, []byte(signingInput))
	sigB64 := base64.RawURLEncoding.EncodeToString(signature)

	return fmt.Sprintf("%s.%s", signingInput, sigB64), nil
}

// GenerateAPIKey gera uma chave M2M com formato eo_live_<32 hex chars>
func GenerateAPIKey() (rawKey string, prefix string, keyHash string) {
	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes)
	hexPart := hex.EncodeToString(bytes)

	rawKey = fmt.Sprintf("eo_live_%s", hexPart)
	prefix = fmt.Sprintf("eo_live_...%s", hexPart[len(hexPart)-6:])
	keyHash = HashKey(rawKey)
	return
}

func HashKey(rawKey string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(rawKey)))
	return hex.EncodeToString(h[:])
}

func ConstantTimeCompareHash(provided, expected string) bool {
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(bytes), err
}

func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// PolicySatisfies verifica se um escopo requerido está contido nas permissões concedidas
// Suporta '*' (curinga total) e prefixos 'namespace:*'
func PolicySatisfies(granted []string, required string) bool {
	for _, g := range granted {
		if g == "*" {
			return true
		}
		if g == required {
			return true
		}
		if strings.HasSuffix(g, ":*") {
			prefix := strings.TrimSuffix(g, ":*")
			if strings.HasPrefix(required, prefix+":") {
				return true
			}
		}
	}
	return false
}

func NowEpoch() int64 {
	return time.Now().Unix()
}
