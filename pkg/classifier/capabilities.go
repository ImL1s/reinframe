package classifier

import "fmt"

// Closed CacheMode values (#132).
const (
	CacheModeNone               = "none"
	CacheModeImplicitPrefix     = "implicit_prefix"
	CacheModeExplicitBreakpoint = "explicit_breakpoint"
	CacheModeExplicitObject     = "explicit_object"
)

// Capability profile names (versioned, trusted config only).
const (
	CapabilitiesProfileGenericNoneV1          = "generic-none-v1"
	CapabilitiesProfileOpenAIOffV1            = "openai-off-v1"
	CapabilitiesProfileOpenAIImplicitV1       = "openai-implicit-v1"
	CapabilitiesProfileOpenAIExplicitPrefixV1 = "openai-explicit-prefix-v1"
	// Anthropic profiles live in anthropic_messages.go constants (#135).
)

// ProviderCapabilities is the closed capability contract for a provider profile.
// Values come only from trusted adapter/profile selection — never from model text.
type ProviderCapabilities struct {
	NativeStructuredOutput bool
	CacheMode              string
	CacheKey               bool
	CacheUsageTelemetry    bool
	StatefulContinuation   bool
	MaxInputBytes          int
}

// LookupCapabilitiesProfile returns a closed profile or fails closed.
func LookupCapabilitiesProfile(name string) (ProviderCapabilities, error) {
	switch name {
	case "", CapabilitiesProfileGenericNoneV1:
		return ProviderCapabilities{
			NativeStructuredOutput: false,
			CacheMode:              CacheModeNone,
			CacheKey:               false,
			CacheUsageTelemetry:    false,
			StatefulContinuation:   false,
			MaxInputBytes:          DefaultMaxInputBytes,
		}, nil
	case CapabilitiesProfileOpenAIOffV1:
		return ProviderCapabilities{
			NativeStructuredOutput: true,
			CacheMode:              CacheModeNone,
			CacheKey:               false,
			CacheUsageTelemetry:    true,
			StatefulContinuation:   false,
			MaxInputBytes:          DefaultMaxInputBytes,
		}, nil
	case CapabilitiesProfileOpenAIImplicitV1:
		return ProviderCapabilities{
			NativeStructuredOutput: true,
			CacheMode:              CacheModeImplicitPrefix,
			CacheKey:               false,
			CacheUsageTelemetry:    true,
			StatefulContinuation:   false,
			MaxInputBytes:          DefaultMaxInputBytes,
		}, nil
	case CapabilitiesProfileOpenAIExplicitPrefixV1:
		return ProviderCapabilities{
			NativeStructuredOutput: true,
			CacheMode:              CacheModeExplicitBreakpoint,
			CacheKey:               true,
			CacheUsageTelemetry:    true,
			StatefulContinuation:   false,
			MaxInputBytes:          DefaultMaxInputBytes,
		}, nil
	case CapabilitiesProfileAnthropicOffV1:
		return ProviderCapabilities{
			NativeStructuredOutput: true,
			CacheMode:              CacheModeNone,
			CacheKey:               false,
			CacheUsageTelemetry:    true,
			StatefulContinuation:   false,
			MaxInputBytes:          DefaultMaxInputBytes,
		}, nil
	case CapabilitiesProfileAnthropicAutomatic5mV1, CapabilitiesProfileAnthropicAutomatic1hV1:
		return ProviderCapabilities{
			NativeStructuredOutput: true,
			CacheMode:              CacheModeImplicitPrefix,
			CacheKey:               false,
			CacheUsageTelemetry:    true,
			StatefulContinuation:   false,
			MaxInputBytes:          DefaultMaxInputBytes,
		}, nil
	case CapabilitiesProfileAnthropicExplicitPrefix5mV1, CapabilitiesProfileAnthropicExplicitPrefix1hV1:
		return ProviderCapabilities{
			NativeStructuredOutput: true,
			CacheMode:              CacheModeExplicitBreakpoint,
			CacheKey:               true,
			CacheUsageTelemetry:    true,
			StatefulContinuation:   false,
			MaxInputBytes:          DefaultMaxInputBytes,
		}, nil
	case CapabilitiesProfileGeminiOffV1:
		return ProviderCapabilities{
			NativeStructuredOutput: true,
			CacheMode:              CacheModeNone,
			CacheKey:               false,
			CacheUsageTelemetry:    true,
			StatefulContinuation:   false,
			MaxInputBytes:          DefaultMaxInputBytes,
		}, nil
	case CapabilitiesProfileGeminiImplicitV1, CapabilitiesProfileGeminiImplicitMin1024V1:
		return ProviderCapabilities{
			NativeStructuredOutput: true,
			CacheMode:              CacheModeImplicitPrefix,
			CacheKey:               false,
			CacheUsageTelemetry:    true,
			StatefulContinuation:   false,
			MaxInputBytes:          DefaultMaxInputBytes,
		}, nil
	case CapabilitiesProfileXAIOffV1:
		return ProviderCapabilities{
			NativeStructuredOutput: true,
			CacheMode:              CacheModeNone,
			CacheKey:               false,
			CacheUsageTelemetry:    true,
			StatefulContinuation:   false,
			MaxInputBytes:          DefaultMaxInputBytes,
		}, nil
	case CapabilitiesProfileXAIResponsesPrefixV1:
		return ProviderCapabilities{
			NativeStructuredOutput: true,
			CacheMode:              CacheModeImplicitPrefix,
			CacheKey:               true,
			CacheUsageTelemetry:    true,
			StatefulContinuation:   false,
			MaxInputBytes:          DefaultMaxInputBytes,
		}, nil
	default:
		return ProviderCapabilities{}, fmt.Errorf("classifier: unknown capabilities_profile %q", boundErr(name))
	}
}

// ValidateCapabilities checks closed enums.
func ValidateCapabilities(c ProviderCapabilities) error {
	switch c.CacheMode {
	case CacheModeNone, CacheModeImplicitPrefix, CacheModeExplicitBreakpoint, CacheModeExplicitObject:
		// ok
	case "":
		return fmt.Errorf("classifier: empty cache_mode")
	default:
		return fmt.Errorf("classifier: unknown cache_mode %q", boundErr(c.CacheMode))
	}
	if c.MaxInputBytes < 0 || c.MaxInputBytes > MaxAllowedInputBytes {
		return fmt.Errorf("classifier: invalid capabilities max_input_bytes")
	}
	return nil
}
