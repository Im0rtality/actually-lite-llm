// Package pricing provides built-in default per-model pricing for known providers.
// Source: https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json
// Ingested: 2026-03-22
package pricing

import (
	_ "embed"
	"encoding/json"
	"log"
)

//go:embed defaults.json
var defaultsJSON []byte

// ModelPrice holds per-million-token pricing (USD).
type ModelPrice struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

// Defaults maps upstream model names to their default pricing.
// Config-level pricing always takes precedence over these defaults.
var Defaults map[string]ModelPrice

func init() {
	if err := json.Unmarshal(defaultsJSON, &Defaults); err != nil {
		log.Fatalf("pricing: failed to parse defaults.json: %v", err)
	}
}
