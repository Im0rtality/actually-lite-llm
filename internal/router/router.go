package router

import (
	"fmt"
	"strings"

	"github.com/im0rtality/actually-lite-llm/internal/config"
	"github.com/im0rtality/actually-lite-llm/internal/pricing"
)

type Route struct {
	Provider              string
	UpstreamModel         string
	CostPerMillionInput   float64
	CostPerMillionOutput  float64
}

type Router struct {
	aliases  map[string]config.ModelAlias
	prefixes []config.RoutingRule
}

func New(models map[string]config.ModelAlias, routing []config.RoutingRule) *Router {
	return &Router{
		aliases:  models,
		prefixes: routing,
	}
}

// Resolve maps a model name to a provider and upstream model name.
func (r *Router) Resolve(model string) (Route, error) {
	// Check alias map first
	if alias, ok := r.aliases[model]; ok {
		route := Route{
			Provider:             alias.Provider,
			UpstreamModel:        alias.Model,
			CostPerMillionInput:  alias.CostPerMillionInput,
			CostPerMillionOutput: alias.CostPerMillionOutput,
		}
		if route.CostPerMillionInput == 0 && route.CostPerMillionOutput == 0 {
			if p, ok := pricing.Defaults[alias.Model]; ok {
				route.CostPerMillionInput = p.Input
				route.CostPerMillionOutput = p.Output
			}
		}
		return route, nil
	}

	// Fall back to prefix rules
	for _, rule := range r.prefixes {
		if strings.HasPrefix(model, rule.Prefix) {
			route := Route{
				Provider:      rule.Provider,
				UpstreamModel: model,
			}
			if p, ok := pricing.Defaults[model]; ok {
				route.CostPerMillionInput = p.Input
				route.CostPerMillionOutput = p.Output
			}
			return route, nil
		}
	}

	return Route{}, fmt.Errorf("no route for model %q", model)
}
