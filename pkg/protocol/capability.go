package protocol

import (
	"errors"
	"fmt"
)

var ErrUnsupportedAgent = errors.New("unsupported agent capability manifest")

// CapabilityFlag represents a uint64 bitmask for individual agent supervision capabilities.
type CapabilityFlag uint64

const (
	// Category 1: Observation & Telemetry Capabilities (Bits 0-4)
	CapEventStream    CapabilityFlag = 1 << iota // 1<<0 (0x1): Real-time NDJSON event streaming
	CapToolInspection                            // 1<<1 (0x2): Tool argument/output inspection
	CapDiffInspection                            // 1<<2 (0x4): File diff and scope inspection
	CapCostTracking                              // 1<<3 (0x8): Token usage and cost tracking
	CapHooks                                     // 1<<4 (0x10): Interception hook execution

	// Category 2: Control & Intervention Capabilities (Bits 5-9)
	CapHeadless   // 1<<5 (0x20): Headless execution control
	CapCLIControl // 1<<6 (0x40): Standard I/O control & signals
	CapPause      // 1<<7 (0x80): Session pause support
	CapCancel     // 1<<8 (0x100): Session cancel support
	CapResume     // 1<<9 (0x200): Session resume support

	// Category 3: Workspace & State Management Capabilities (Bits 10-14)
	CapCheckpoint // 1<<10 (0x400): Worktree snapshot creation
	CapRollback   // 1<<11 (0x800): Worktree snapshot restoration
	CapMCP        // 1<<12 (0x1000): Model Context Protocol isolation
	CapSubagents  // 1<<13 (0x2000): Child process/subagent spawning
	CapExtensions // 1<<14 (0x4000): Harness extension plugin loading

	// Category 4: Model & Provider Management Capabilities (Bits 15-19)
	CapSwitchModel    // 1<<15 (0x8000): Runtime model switching
	CapCustomProvider // 1<<16 (0x10000): Custom LLM provider integration
	CapOpenAICompat   // 1<<17 (0x20000): OpenAI API protocol compliance
	CapLocalModels    // 1<<18 (0x40000): Local LLM integration (Ollama/llama.cpp)
	CapSDK            // 1<<19 (0x80000): Native language SDK binding support
)

const (
	Level0RequiredMask uint64 = uint64(CapEventStream)
	Level1RequiredMask uint64 = Level0RequiredMask | uint64(CapToolInspection) | uint64(CapPause) | uint64(CapCancel) | uint64(CapResume)
	Level2RequiredMask uint64 = Level1RequiredMask | uint64(CapDiffInspection) | uint64(CapCheckpoint) | uint64(CapRollback)
	Level3RequiredMask uint64 = Level2RequiredMask | uint64(CapHeadless) | uint64(CapCLIControl) | uint64(CapMCP) | uint64(CapSubagents) | uint64(CapSwitchModel)
)

var flagToStringMap = map[CapabilityFlag]string{
	CapEventStream:    "CapEventStream",
	CapToolInspection: "CapToolInspection",
	CapDiffInspection: "CapDiffInspection",
	CapCostTracking:   "CapCostTracking",
	CapHooks:          "CapHooks",
	CapHeadless:       "CapHeadless",
	CapCLIControl:     "CapCLIControl",
	CapPause:          "CapPause",
	CapCancel:         "CapCancel",
	CapResume:         "CapResume",
	CapCheckpoint:     "CapCheckpoint",
	CapRollback:       "CapRollback",
	CapMCP:            "CapMCP",
	CapSubagents:      "CapSubagents",
	CapExtensions:     "CapExtensions",
	CapSwitchModel:    "CapSwitchModel",
	CapCustomProvider: "CapCustomProvider",
	CapOpenAICompat:   "CapOpenAICompat",
	CapLocalModels:    "CapLocalModels",
	CapSDK:            "CapSDK",
}

// String returns the string representation of a CapabilityFlag.
func (f CapabilityFlag) String() string {
	if name, ok := flagToStringMap[f]; ok {
		return name
	}
	return fmt.Sprintf("CapabilityFlag(0x%x)", uint64(f))
}

// FlagToString converts a CapabilityFlag to its string representation.
func FlagToString(flag CapabilityFlag) string {
	return flag.String()
}

// HandshakeRequest represents a client handshake request for session initialization.
type HandshakeRequest struct {
	SessionID      string             `json:"session_id"`
	RequestedLevel int                `json:"requested_level"`
	Manifest       CapabilityManifest `json:"manifest"`
}

// HandshakeResponse represents the supervisor response to a handshake request.
type HandshakeResponse struct {
	SessionID       string   `json:"session_id"`
	NegotiatedLevel int      `json:"negotiated_level"`
	IsDegraded      bool     `json:"is_degraded"`
	DegradedFrom    int      `json:"degraded_from,omitempty"`
	MissingFlags    []string `json:"missing_flags,omitempty"`
}

// ToBitmask combines boolean capability flags and IntegrationLevel defaults into a uint64 bitmask.
func (m CapabilityManifest) ToBitmask() uint64 {
	if m.hasRawBitmask {
		return m.rawBitmask
	}

	var mask uint64

	if m.SupportsPause {
		mask |= uint64(CapPause)
	}
	if m.SupportsCancel {
		mask |= uint64(CapCancel)
	}
	if m.SupportsResume {
		mask |= uint64(CapResume)
	}
	if m.SupportsCheckpoint {
		mask |= uint64(CapCheckpoint)
	}
	if m.SupportsRollback {
		mask |= uint64(CapRollback)
	}
	if m.SupportsMCP {
		mask |= uint64(CapMCP)
	}

	switch m.IntegrationLevel {
	case 3:
		mask |= Level3RequiredMask
	case 2:
		mask |= Level2RequiredMask
	case 1:
		mask |= Level1RequiredMask
	case 0:
		mask |= Level0RequiredMask
	default:
		if m.IntegrationLevel > 3 {
			mask |= Level3RequiredMask
		} else if m.IntegrationLevel >= 0 {
			mask |= Level0RequiredMask
		}
	}

	return mask
}

// FromBitmask populates a CapabilityManifest struct from a bitmask.
func FromBitmask(mask uint64) CapabilityManifest {
	manifest := CapabilityManifest{
		SupportsPause:      (mask & uint64(CapPause)) != 0,
		SupportsCancel:     (mask & uint64(CapCancel)) != 0,
		SupportsResume:     (mask & uint64(CapResume)) != 0,
		SupportsCheckpoint: (mask & uint64(CapCheckpoint)) != 0,
		SupportsRollback:   (mask & uint64(CapRollback)) != 0,
		SupportsMCP:        (mask & uint64(CapMCP)) != 0,
		rawBitmask:         mask,
		hasRawBitmask:      true,
	}
	manifest.IntegrationLevel = EvaluateAchievableLevelFromMask(mask)
	return manifest
}

// HasCapability checks if a specific capability flag is set in the manifest.
func (m CapabilityManifest) HasCapability(flag CapabilityFlag) bool {
	return (m.ToBitmask() & uint64(flag)) == uint64(flag)
}

// EvaluateAchievableLevel determines the maximum achievable supervision level (Level 0-3) for a manifest.
func EvaluateAchievableLevel(manifest *CapabilityManifest) int {
	if manifest == nil {
		return -1
	}
	return EvaluateAchievableLevelFromMask(manifest.ToBitmask())
}

// EvaluateAchievableLevelFromMask determines the maximum achievable supervision level (Level 0-3) from a raw bitmask.
func EvaluateAchievableLevelFromMask(mask uint64) int {
	if (mask & Level3RequiredMask) == Level3RequiredMask {
		return 3
	}
	if (mask & Level2RequiredMask) == Level2RequiredMask {
		return 2
	}
	if (mask & Level1RequiredMask) == Level1RequiredMask {
		return 1
	}
	if (mask & Level0RequiredMask) == Level0RequiredMask {
		return 0
	}
	return -1
}

// NegotiateLevel evaluates a handshake request, returning a HandshakeResponse with automatic degradation if needed.
func NegotiateLevel(req *HandshakeRequest) (*HandshakeResponse, error) {
	if req == nil {
		return nil, errors.New("handshake request cannot be nil")
	}
	if req.SessionID == "" {
		return nil, errors.New("session_id cannot be empty")
	}
	if req.RequestedLevel < 0 || req.RequestedLevel > 3 {
		return nil, fmt.Errorf("invalid requested level: %d (must be 0-3)", req.RequestedLevel)
	}

	achievable := EvaluateAchievableLevel(&req.Manifest)
	if achievable < 0 {
		return nil, ErrUnsupportedAgent
	}

	if req.RequestedLevel <= achievable {
		return &HandshakeResponse{
			SessionID:       req.SessionID,
			NegotiatedLevel: req.RequestedLevel,
			IsDegraded:      false,
			DegradedFrom:    0,
			MissingFlags:    nil,
		}, nil
	}

	manifestMask := req.Manifest.ToBitmask()
	missingFlags := make([]string, 0)

	for i := 0; i < 20; i++ {
		flag := CapabilityFlag(1 << uint(i))
		if isRequiredForLevel(flag, req.RequestedLevel) && (manifestMask&uint64(flag)) == 0 {
			missingFlags = append(missingFlags, flag.String())
		}
	}

	return &HandshakeResponse{
		SessionID:       req.SessionID,
		NegotiatedLevel: achievable,
		IsDegraded:      true,
		DegradedFrom:    req.RequestedLevel,
		MissingFlags:    missingFlags,
	}, nil
}

func isRequiredForLevel(flag CapabilityFlag, level int) bool {
	var requiredMask uint64
	switch level {
	case 0:
		requiredMask = Level0RequiredMask
	case 1:
		requiredMask = Level1RequiredMask
	case 2:
		requiredMask = Level2RequiredMask
	case 3:
		requiredMask = Level3RequiredMask
	default:
		return false
	}
	return (requiredMask & uint64(flag)) != 0
}
