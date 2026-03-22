package auth

import (
	"testing"

	"github.com/im0rtality/actually-lite-llm/internal/config"
)

func TestLookup(t *testing.T) {
	a := New([]config.APIKey{
		{Key: "key1", App: "app1", AllowedModels: []string{"*"}},
		{Key: "key2", App: "app2", AllowedModels: []string{"gpt-4o"}},
	})

	if ki := a.Lookup("key1"); ki == nil || ki.App != "app1" {
		t.Fatal("expected to find key1")
	}
	if ki := a.Lookup("unknown"); ki != nil {
		t.Fatal("expected nil for unknown key")
	}
}

func TestAllowsModel(t *testing.T) {
	ki := &KeyInfo{AllowedModels: []string{"gpt-4o", "claude-sonnet"}}
	if !ki.AllowsModel("gpt-4o") {
		t.Error("should allow gpt-4o")
	}
	if ki.AllowsModel("gpt-4-turbo") {
		t.Error("should not allow gpt-4-turbo")
	}

	wildcard := &KeyInfo{AllowedModels: []string{"*"}}
	if !wildcard.AllowsModel("anything") {
		t.Error("wildcard should allow anything")
	}
}

func TestExtractBearer(t *testing.T) {
	if got := ExtractBearer("Bearer mytoken"); got != "mytoken" {
		t.Errorf("got %q", got)
	}
	if got := ExtractBearer("Basic abc"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
	if got := ExtractBearer(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}
