// Package agents wires the Google ADK runtime: models, the guard callbacks
// that enforce read-only allowlists and budgets, the compiler from workflow
// YAML to an agent tree, and the Investigator the worker drives.
package agents

import (
	"context"
	"fmt"
	"os"
	"sync"

	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"

	"github.com/devtron-labs/devtron-sre-agent/internal/config"
)

// ModelFactory resolves workflow model aliases (fast, strong) to model.LLM
// instances. Fixed entries override construction, which is how tests inject a
// scripted model and how a tenant could pin a specific model later.
type ModelFactory struct {
	cfg   *config.Config
	mu    sync.Mutex
	cache map[string]model.LLM
}

// NewModelFactory builds a factory from platform config.
func NewModelFactory(cfg *config.Config) *ModelFactory {
	return &ModelFactory{cfg: cfg, cache: map[string]model.LLM{}}
}

// Get returns the model for an alias, constructing it once.
func (f *ModelFactory) Get(ctx context.Context, alias string) (model.LLM, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.cache[alias]; ok {
		return m, nil
	}
	id := f.modelID(alias)
	var (
		m   model.LLM
		err error
	)
	switch f.cfg.Models.Provider {
	case "gemini", "":
		m, err = newGemini(ctx, id)
	case "anthropic", "claude":
		// The strong alias does the analysis and the critique, which is where
		// reasoning earns its cost; the fast alias classifies.
		m, err = NewAnthropic(AnthropicOptions{
			ModelID:  id,
			Thinking: alias != "fast",
			Effort:   f.cfg.Models.Effort,
		})
	default:
		err = fmt.Errorf("unknown model provider %q", f.cfg.Models.Provider)
	}
	if err != nil {
		return nil, fmt.Errorf("model %s (%s): %w", alias, id, err)
	}
	f.cache[alias] = m
	return m, nil
}

func (f *ModelFactory) modelID(alias string) string {
	switch alias {
	case "fast":
		if f.cfg.Models.Fast != "" {
			return f.cfg.Models.Fast
		}
		return defaultModel(f.cfg.Models.Provider, alias)
	case "strong":
		if f.cfg.Models.Strong != "" {
			return f.cfg.Models.Strong
		}
		return defaultModel(f.cfg.Models.Provider, alias)
	}
	return alias // a literal model id
}

// defaultModel picks a sensible model per provider when config leaves the
// alias empty.
func defaultModel(provider, alias string) string {
	switch provider {
	case "anthropic", "claude":
		return defaultAnthropicModel
	}
	if alias == "fast" {
		return "gemini-2.5-flash"
	}
	return "gemini-2.5-pro"
}

func newGemini(ctx context.Context, id string) (model.LLM, error) {
	cc := &genai.ClientConfig{}
	for _, k := range []string{"SRE_GEMINI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY"} {
		if v := os.Getenv(k); v != "" {
			cc.APIKey = v
			break
		}
	}
	if cc.APIKey == "" && os.Getenv("GOOGLE_GENAI_USE_VERTEXAI") == "" {
		return nil, fmt.Errorf("no Gemini credentials: set SRE_GEMINI_API_KEY or configure Vertex AI (GOOGLE_GENAI_USE_VERTEXAI, GOOGLE_CLOUD_PROJECT, GOOGLE_CLOUD_LOCATION)")
	}
	return gemini.NewModel(ctx, id, cc)
}
