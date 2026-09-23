// Package config loads configuration from an optional YAML file and SRE_*
// environment variables. Environment always wins over the file.
//
// One binary, one config. The agent reaches every cluster through the Devtron
// orchestrator API, so there is no kubeconfig, no cluster registry and no
// per-cluster credential here: a Devtron URL and one view-only API token are
// the whole of the access configuration.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the full configuration.
type Config struct {
	HTTP struct {
		Addr string `yaml:"addr"`
	} `yaml:"http"`

	Database struct {
		URL            string `yaml:"url"`
		MaxConns       int32  `yaml:"maxConns"`
		MigrateOnStart bool   `yaml:"migrateOnStart"`
	} `yaml:"database"`

	Log struct {
		Level  string `yaml:"level"`  // debug | info | warn | error
		Format string `yaml:"format"` // json | text
	} `yaml:"log"`

	// Devtron is the single access point to every managed cluster.
	Devtron struct {
		// URL is the Devtron host, e.g. https://devtron.example.com.
		URL string `yaml:"url"`
		// Token is a view-only Devtron API token. It is sent three different
		// ways depending on the endpoint; see the devtron package.
		Token string `yaml:"token"`
		// IntelligencePath is where the Athena one-shot debugger is mounted.
		IntelligencePath string `yaml:"intelligencePath"`
		// TimeoutSeconds bounds a normal REST call.
		TimeoutSeconds int `yaml:"timeoutSeconds"`
		// InsecureTLS skips certificate verification. Test installs only.
		InsecureTLS bool `yaml:"insecureTls"`
	} `yaml:"devtron"`

	// Models is provider-agnostic. Leave Provider empty and whichever
	// credential is present decides, so a deployment that only has an
	// Anthropic key never needs to say so twice.
	Models struct {
		// Provider is anthropic or gemini. Empty means auto-detect.
		Provider string `yaml:"provider"`
		// Fast is the judge's model, Strong the SRE's. Either may be any
		// model id the chosen provider accepts; empty picks that provider's
		// sensible default.
		Fast   string `yaml:"fast"`
		Strong string `yaml:"strong"`
		// Effort is the Anthropic thinking effort: low, medium, high, xhigh
		// or max. Ignored by other providers.
		Effort string `yaml:"effort"`
	} `yaml:"models"`

	Run struct {
		// Concurrency is how many investigations run at once in this process.
		Concurrency int `yaml:"concurrency"`
		// IntelligenceTimeoutSeconds bounds the /intelligence SSE stream.
		IntelligenceTimeoutSeconds int `yaml:"intelligenceTimeoutSeconds"`
		// TimeoutSeconds bounds a whole run.
		TimeoutSeconds int `yaml:"timeoutSeconds"`
		// MaxToolCalls and MaxModelTokens are the agent budget.
		MaxToolCalls   int `yaml:"maxToolCalls"`
		MaxModelTokens int `yaml:"maxModelTokens"`
	} `yaml:"run"`

	Features map[string]bool `yaml:"features"`
}

// Load reads the config file named by SRE_CONFIG (or ./config.yaml when it
// exists), applies environment overrides and validates the result.
func Load() (*Config, error) {
	// Read .env before anything looks at the environment. SRE_DOTENV points
	// elsewhere; SRE_DOTENV=off skips it entirely.
	if p := os.Getenv("SRE_DOTENV"); p != "off" {
		if p == "" {
			p = ".env"
		}
		loadDotEnv(p)
	}

	c := defaults()
	path := os.Getenv("SRE_CONFIG")
	if path == "" {
		if _, err := os.Stat("config.yaml"); err == nil {
			path = "config.yaml"
		}
	}
	if path != "" {
		b, err := os.ReadFile(path) //nolint:gosec // the operator chooses the config path
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		if err := yaml.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	applyEnv(c)
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func defaults() *Config {
	c := &Config{}
	c.HTTP.Addr = ":8090"
	c.Database.MaxConns = 10
	c.Database.MigrateOnStart = true
	c.Log.Level = "info"
	c.Log.Format = "text"
	c.Devtron.IntelligencePath = "/proxy/athena/intelligence"
	c.Devtron.TimeoutSeconds = 30
	c.Run.Concurrency = 4
	c.Run.IntelligenceTimeoutSeconds = 300
	c.Run.TimeoutSeconds = 900
	// Measured: on runs that succeeded, Devtron's first pass took 12-100s and
	// our own two agents took 170-230s. The agents are the bottleneck, and a
	// tool budget is the only thing that bounds them — a ceiling of 25 is a
	// ceiling the model will happily spend. Twelve is roughly double what a
	// well-aimed investigation actually uses; SRE_RUN_MAX_TOOL_CALLS raises it
	// when a specific incident needs the room.
	c.Run.MaxToolCalls = 12
	c.Run.MaxModelTokens = 400000
	c.Features = map[string]bool{}
	return c
}

func applyEnv(c *Config) {
	set(&c.HTTP.Addr, "SRE_HTTP_ADDR")
	set(&c.Database.URL, "SRE_DATABASE_URL")
	setBool(&c.Database.MigrateOnStart, "SRE_DATABASE_MIGRATE")
	set(&c.Log.Level, "SRE_LOG_LEVEL")
	set(&c.Log.Format, "SRE_LOG_FORMAT")

	set(&c.Devtron.URL, "SRE_DEVTRON_URL")
	set(&c.Devtron.Token, "SRE_DEVTRON_TOKEN")
	set(&c.Devtron.IntelligencePath, "SRE_DEVTRON_INTELLIGENCE_PATH")
	setInt(&c.Devtron.TimeoutSeconds, "SRE_DEVTRON_TIMEOUT_SECONDS")
	setBool(&c.Devtron.InsecureTLS, "SRE_DEVTRON_INSECURE_TLS")

	set(&c.Models.Provider, "SRE_MODELS_PROVIDER")
	set(&c.Models.Fast, "SRE_MODELS_FAST")
	set(&c.Models.Strong, "SRE_MODELS_STRONG")
	set(&c.Models.Effort, "SRE_MODELS_EFFORT")

	setInt(&c.Run.Concurrency, "SRE_RUN_CONCURRENCY")
	setInt(&c.Run.IntelligenceTimeoutSeconds, "SRE_RUN_INTELLIGENCE_TIMEOUT_SECONDS")
	setInt(&c.Run.TimeoutSeconds, "SRE_RUN_TIMEOUT_SECONDS")
	setInt(&c.Run.MaxToolCalls, "SRE_RUN_MAX_TOOL_CALLS")
	setInt(&c.Run.MaxModelTokens, "SRE_RUN_MAX_MODEL_TOKENS")

	// SRE_FEATURE_<NAME>=true enables a feature flag.
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(k, "SRE_FEATURE_") {
			continue
		}
		name := strings.ToLower(strings.TrimPrefix(k, "SRE_FEATURE_"))
		c.Features[name] = v == "true" || v == "1"
	}
}

func (c *Config) validate() error {
	// The Devtron host and token are optional at boot: they can also be set
	// from the settings screen, and refusing to start would leave an operator
	// with no way to reach that screen.
	if c.Devtron.URL != "" &&
		!strings.HasPrefix(c.Devtron.URL, "http://") && !strings.HasPrefix(c.Devtron.URL, "https://") {
		return fmt.Errorf("devtron.url must include a scheme, got %q", c.Devtron.URL)
	}
	if c.Database.URL == "" {
		return fmt.Errorf("database.url is required (SRE_DATABASE_URL)")
	}
	if c.Models.Provider == "" {
		c.Models.Provider = detectProvider()
	}
	switch c.Models.Provider {
	case "gemini", "anthropic", "claude":
	default:
		return fmt.Errorf("models.provider must be anthropic or gemini, got %q", c.Models.Provider)
	}
	c.resolveModelDefaults()
	if c.Run.Concurrency < 1 {
		c.Run.Concurrency = 1
	}
	return nil
}

// resolveModelDefaults fills in the model ids the agents will use, so the
// config endpoint and the startup log report the real values rather than the
// empty strings that only mean "not overridden".
func (c *Config) resolveModelDefaults() {
	anthropic := c.Models.Provider == "anthropic" || c.Models.Provider == "claude"
	if c.Models.Fast == "" {
		c.Models.Fast = "gemini-2.5-flash"
		if anthropic {
			c.Models.Fast = "claude-sonnet-5"
		}
	}
	if c.Models.Strong == "" {
		c.Models.Strong = "gemini-2.5-pro"
		if anthropic {
			c.Models.Strong = "claude-opus-5"
		}
	}
}

// providerKeys maps a provider to the environment variables that carry its
// credential, most specific first.
var providerKeys = map[string][]string{
	"anthropic": {"SRE_ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY"},
	"gemini":    {"SRE_GEMINI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY"},
}

// detectProvider picks a provider from whichever credential is present.
// Anthropic is checked first because it is the usual choice for this kind of
// judgement-heavy work; with both keys set, configure the one you want
// explicitly rather than relying on this order.
func detectProvider() string {
	for _, p := range []string{"anthropic", "gemini"} {
		if envAny(providerKeys[p]...) != "" {
			return p
		}
	}
	if os.Getenv("GOOGLE_GENAI_USE_VERTEXAI") != "" {
		return "gemini"
	}
	// Nothing configured. Name anthropic so the credential error that follows
	// names one provider rather than listing every possibility.
	return "anthropic"
}

// ModelCredentialMissing returns why no model can be reached, or "" when one
// can.
//
// This is a warning at boot rather than a fatal error: refusing to start
// would make the settings screen unreachable, and an operator with a
// half-configured deployment needs the UI more than anyone. A run that
// actually needs the credential fails with this same sentence.
func (c *Config) ModelCredentialMissing() string {
	if err := c.checkModelCredentials(); err != nil {
		return err.Error()
	}
	return ""
}

func (c *Config) checkModelCredentials() error {
	provider := c.Models.Provider
	if provider == "claude" {
		provider = "anthropic"
	}
	if envAny(providerKeys[provider]...) != "" {
		return nil
	}
	if provider == "gemini" && os.Getenv("GOOGLE_GENAI_USE_VERTEXAI") != "" {
		return nil
	}
	return fmt.Errorf("models.provider is %q but no credential is set; export %s",
		provider, strings.Join(providerKeys[provider], " or "))
}

func envAny(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// Enabled reports a feature flag.
func (c *Config) Enabled(feature string) bool { return c.Features[feature] }

func set(dst *string, key string) {
	if v := os.Getenv(key); v != "" {
		*dst = v
	}
}

func setBool(dst *bool, key string) {
	if v := os.Getenv(key); v != "" {
		*dst = v == "true" || v == "1"
	}
}

func setInt(dst *int, key string) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*dst = n
		}
	}
}
