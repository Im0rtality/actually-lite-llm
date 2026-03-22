package auth

import (
	"strings"

	"github.com/im0rtality/actually-lite-llm/internal/config"
)

type KeyInfo struct {
	App           string
	AllowedModels []string
}

type Authenticator struct {
	keys map[string]KeyInfo
}

func New(apiKeys []config.APIKey) *Authenticator {
	m := make(map[string]KeyInfo, len(apiKeys))
	for _, k := range apiKeys {
		m[k.Key] = KeyInfo{
			App:           k.App,
			AllowedModels: k.AllowedModels,
		}
	}
	return &Authenticator{keys: m}
}

// Lookup returns the KeyInfo for the given bearer token.
// Returns nil if the key is not found.
func (a *Authenticator) Lookup(token string) *KeyInfo {
	info, ok := a.keys[token]
	if !ok {
		return nil
	}
	return &info
}

// AllowsModel reports whether the key permits the given model.
func (ki *KeyInfo) AllowsModel(model string) bool {
	for _, m := range ki.AllowedModels {
		if m == "*" || m == model {
			return true
		}
	}
	return false
}

// ExtractBearer extracts the token from an "Authorization: Bearer <token>" header.
func ExtractBearer(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimPrefix(header, prefix)
}
