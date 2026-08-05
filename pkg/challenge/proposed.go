package challenge

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

// ValidateProposedForChallenge fail-closes on lossy or incomplete projections.
// Truncated commands/prefixes must not create or consume an appealable challenge.
func ValidateProposedForChallenge(pa adapter.ProposedAction) error {
	if strings.TrimSpace(pa.SchemaVersion) == "" {
		return fmt.Errorf("proposed_action: schema_version required")
	}
	if pa.SchemaVersion != adapter.ProposedActionSchemaVersion {
		return fmt.Errorf("proposed_action: unsupported schema_version %q", pa.SchemaVersion)
	}
	if pa.Truncated {
		return fmt.Errorf("proposed_action: truncated projections cannot open/consume challenges")
	}
	ps := strings.TrimSpace(pa.ParseStatus)
	if ps == "" {
		return fmt.Errorf("proposed_action: parse_status required (must be ok)")
	}
	if ps != "ok" {
		return fmt.Errorf("proposed_action: parse_status %q cannot open/consume challenges", ps)
	}
	if strings.TrimSpace(pa.SessionID) == "" {
		return fmt.Errorf("proposed_action: session_id required")
	}
	if strings.TrimSpace(pa.ToolName) == "" {
		return fmt.Errorf("proposed_action: tool_name required")
	}
	if strings.TrimSpace(pa.ToolClass) == "" {
		return fmt.Errorf("proposed_action: tool_class required")
	}
	if strings.TrimSpace(pa.ActionID) == "" {
		return fmt.Errorf("proposed_action: action_id required")
	}
	if utf8.RuneCountInString(pa.ActionID) > adapter.MaxProposedActionIDRunes {
		return fmt.Errorf("proposed_action: action_id exceeds max runes")
	}
	if utf8.RuneCountInString(pa.Command) > adapter.MaxProposedCommandRunes {
		return fmt.Errorf("proposed_action: command exceeds max runes")
	}
	if utf8.RuneCountInString(pa.FilePath) > adapter.MaxProposedFilePathRunes {
		return fmt.Errorf("proposed_action: file_path exceeds max runes")
	}
	if len(pa.Arguments) > adapter.MaxProposedArgs {
		return fmt.Errorf("proposed_action: too many arguments")
	}
	// Edit/write must have enough structured surface for an operation digest.
	if pa.ToolClass == adapter.ToolClassEdit {
		if _, err := editOperationDigest(pa); err != nil {
			return err
		}
	}
	return nil
}
