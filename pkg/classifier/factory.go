package classifier

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ImL1s/reinframe/pkg/config"
)

// ProviderFactoryOptions injects test/runtime dependencies only.
// Production callers leave these nil. AllowRemote must remain false in production.
type ProviderFactoryOptions struct {
	HTTPClient  *http.Client
	LookupEnv   func(string) (string, bool)
	Sleep       func(context.Context, time.Duration) error
	Now         func() time.Time
	AllowRemote bool
}

// NewClassifierProviderFromConfig maps config.ClassifierProviderConfig to a ClassifierProvider.
//
//	kind empty/none         → FakeClassifierProvider (no network)
//	kind openai_compatible  → OpenAICompatibleProvider (loopback-only by default)
//	kind openai_responses   → OpenAIResponsesProvider (native Responses; #134)
//	kind anthropic_messages → AnthropicMessagesProvider (native Messages; #135)
//
// Loading a YAML file does not automatically wire a provider unless the process
// calls this factory (or equivalent).
func NewClassifierProviderFromConfig(cfg config.ClassifierProviderConfig, opts ProviderFactoryOptions) (ClassifierProvider, error) {
	tmp := config.Default()
	tmp.ClassifierProvider = cfg
	if err := tmp.Validate(); err != nil {
		return nil, fmt.Errorf("classifier factory: config invalid")
	}

	// Use the same normalized kind as Validate (single source of truth).
	switch cfg.NormalizeKind() {
	case "none":
		return FakeClassifierProvider{}, nil
	case "openai_compatible":
		oai := OpenAICompatibleConfig{
			Kind:                "openai_compatible",
			Model:               cfg.Model,
			BaseURL:             cfg.BaseURL,
			Path:                cfg.Path,
			APIKeyRef:           cfg.APIKeyRef,
			Timeout:             time.Duration(cfg.TimeoutMS) * time.Millisecond,
			MaxInputBytes:       cfg.MaxInputBytes,
			MaxOutputBytes:      cfg.MaxOutputBytes,
			CapabilitiesProfile: cfg.CapabilitiesProfile,
			HTTPClient:          opts.HTTPClient,
			LookupEnv:           opts.LookupEnv,
			Sleep:               opts.Sleep,
			Now:                 opts.Now,
			AllowRemote:         opts.AllowRemote,
		}
		return NewOpenAICompatible(oai)
	case KindOpenAIResponses:
		path := cfg.Path
		if strings.TrimSpace(path) == "" {
			path = DefaultOpenAIResponsesPath
		}
		resp := OpenAIResponsesConfig{
			Kind:                KindOpenAIResponses,
			Model:               cfg.Model,
			BaseURL:             cfg.BaseURL,
			Path:                path,
			APIKeyRef:           cfg.APIKeyRef,
			Timeout:             time.Duration(cfg.TimeoutMS) * time.Millisecond,
			MaxInputBytes:       cfg.MaxInputBytes,
			MaxOutputBytes:      cfg.MaxOutputBytes,
			CapabilitiesProfile: cfg.CapabilitiesProfile,
			EgressProfile:       cfg.EgressProfile,
			HTTPClient:          opts.HTTPClient,
			LookupEnv:           opts.LookupEnv,
			Sleep:               opts.Sleep,
			Now:                 opts.Now,
			AllowRemote:         opts.AllowRemote,
		}
		return NewOpenAIResponses(resp)
	case KindAnthropicMessages:
		path := cfg.Path
		if strings.TrimSpace(path) == "" {
			path = DefaultAnthropicMessagesPath
		}
		anth := AnthropicMessagesConfig{
			Kind:                KindAnthropicMessages,
			Model:               cfg.Model,
			BaseURL:             cfg.BaseURL,
			Path:                path,
			APIKeyRef:           cfg.APIKeyRef,
			Platform:            cfg.Platform,
			Timeout:             time.Duration(cfg.TimeoutMS) * time.Millisecond,
			MaxInputBytes:       cfg.MaxInputBytes,
			MaxOutputBytes:      cfg.MaxOutputBytes,
			CapabilitiesProfile: cfg.CapabilitiesProfile,
			EgressProfile:       cfg.EgressProfile,
			HTTPClient:          opts.HTTPClient,
			LookupEnv:           opts.LookupEnv,
			Sleep:               opts.Sleep,
			Now:                 opts.Now,
			AllowRemote:         opts.AllowRemote,
		}
		return NewAnthropicMessages(anth)
	default:
		return nil, fmt.Errorf("classifier factory: unknown kind")
	}
}
