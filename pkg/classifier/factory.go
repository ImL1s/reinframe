package classifier

import (
	"context"
	"fmt"
	"net/http"
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
//	kind empty/none     → FakeClassifierProvider (no network)
//	kind openai_compatible → OpenAICompatibleProvider (loopback-only by default)
//
// Loading a YAML file does not automatically wire a provider unless the process
// calls this factory (or equivalent).
func NewClassifierProviderFromConfig(cfg config.ClassifierProviderConfig, opts ProviderFactoryOptions) (ClassifierProvider, error) {
	tmp := config.Default()
	tmp.ClassifierProvider = cfg
	if err := tmp.Validate(); err != nil {
		return nil, fmt.Errorf("classifier factory: config invalid")
	}

	switch cfg.Kind {
	case "", "none":
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
	default:
		return nil, fmt.Errorf("classifier factory: unknown kind")
	}
}
