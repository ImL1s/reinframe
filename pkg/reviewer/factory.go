package reviewer

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/ImL1s/reinframe/pkg/config"
)

// ErrRemoteBlocked is returned when remote reviewer modes are requested while
// session.local_only_reviewer is true (ADR 003 default).
var ErrRemoteBlocked = fmt.Errorf("reviewer: remote mode blocked by local_only_reviewer; set session.local_only_reviewer=false to opt in")

// ErrLoopbackRequired is returned when local mode uses a non-loopback BaseURL.
var ErrLoopbackRequired = fmt.Errorf("reviewer: local mode requires loopback BaseURL (127.0.0.1, localhost, ::1)")

// NewProviderFromConfig constructs a ReviewerProvider from Reinframe config.
//
// Modes:
//   - "local": OpenAI-compatible client to a loopback BaseURL only (required).
//   - "openai_compatible" / "cloud": remote allowed only when !LocalOnlyReviewer.
//
// API keys are resolved from Reviewer.APIKeyRef env placeholders only.
// High-confidence policy paths never call this provider; only uncertain slow path does.
func NewProviderFromConfig(cfg config.Config) (ReviewerProvider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	mode := strings.TrimSpace(cfg.Reviewer.Mode)
	if mode == "" {
		mode = "local"
	}
	localOnly := cfg.Session.LocalOnlyReviewer

	switch mode {
	case "local":
		base := strings.TrimSpace(cfg.Reviewer.BaseURL)
		if base == "" {
			return nil, fmt.Errorf("reviewer: local mode requires reviewer.base_url pointing at a loopback OpenAI-compatible endpoint")
		}
		if !isLoopbackBaseURL(base) {
			return nil, ErrLoopbackRequired
		}
		return newOAIFromReviewer(cfg.Reviewer, base)
	case "openai_compatible", "cloud":
		if localOnly {
			return nil, ErrRemoteBlocked
		}
		base := strings.TrimSpace(cfg.Reviewer.BaseURL)
		if base == "" {
			return nil, fmt.Errorf("reviewer: %s mode requires reviewer.base_url", mode)
		}
		return newOAIFromReviewer(cfg.Reviewer, base)
	default:
		return nil, fmt.Errorf("reviewer: unsupported mode %q", mode)
	}
}

func newOAIFromReviewer(r config.ReviewerConfig, base string) (*OpenAICompatibleProvider, error) {
	key, err := resolveAPIKey(r.APIKeyRef)
	if err != nil {
		return nil, err
	}
	return NewOpenAICompatible(OpenAICompatibleConfig{
		BaseURL: base,
		Model:   r.Model,
		APIKey:  key,
	})
}

func resolveAPIKey(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	if !strings.HasPrefix(ref, config.EnvPlaceholderPrefix) || !strings.HasSuffix(ref, config.EnvPlaceholderSuffix) {
		return "", fmt.Errorf("reviewer: api_key_ref must be env placeholder like ${NAME}")
	}
	name := strings.TrimSuffix(strings.TrimPrefix(ref, config.EnvPlaceholderPrefix), config.EnvPlaceholderSuffix)
	if name == "" {
		return "", fmt.Errorf("reviewer: empty api_key_ref env name")
	}
	return os.Getenv(name), nil
}

func isLoopbackBaseURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		if h, _, err := net.SplitHostPort(raw); err == nil {
			host = h
		} else {
			host = raw
		}
	}
	host = strings.ToLower(host)
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
