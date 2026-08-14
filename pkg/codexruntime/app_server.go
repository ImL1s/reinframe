package codexruntime

import (
	"time"

	"github.com/ImL1s/reinframe/pkg/config"
)

// AppServerBridgeConfig holds validated configuration for the Codex App Server runtime bridge (#184).
type AppServerBridgeConfig struct {
	Executable                 string        `json:"executable"`
	Args                       []string      `json:"args"`
	WorkDir                    string        `json:"work_dir,omitempty"`
	StartupTimeout             time.Duration `json:"startup_timeout"`
	RequestTimeout             time.Duration `json:"request_timeout"`
	MaxMessageBytes            int           `json:"max_message_bytes"`
	AllowProviderModelFallback bool          `json:"allow_provider_model_fallback"`
}

// BuildAppServerConfig derives an AppServerBridgeConfig from CodexRuntimeConfig and a verified ResolvedBinary.
// It strictly enforces fail-closed configuration invariants and discrete argv creation.
func BuildAppServerConfig(cfg config.CodexRuntimeConfig, bin *ResolvedBinary) (AppServerBridgeConfig, error) {
	if !cfg.Enabled {
		return AppServerBridgeConfig{}, ErrRuntimeDisabled
	}
	if bin == nil || bin.Path == "" {
		return AppServerBridgeConfig{}, ErrRuntimeUnavailable
	}

	startupTimeout := 15 * time.Second
	if cfg.StatusCheckTimeoutMS > 0 {
		startupTimeout = time.Duration(cfg.StatusCheckTimeoutMS) * time.Millisecond
	}

	return AppServerBridgeConfig{
		Executable:                 bin.Path,
		Args:                       []string{"app-server"},
		StartupTimeout:             startupTimeout,
		RequestTimeout:             30 * time.Second,
		MaxMessageBytes:            1048576, // 1MB (#184 max_message_bytes)
		AllowProviderModelFallback: false,   // default fail-closed model selection
	}, nil
}
