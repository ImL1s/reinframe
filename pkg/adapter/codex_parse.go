package adapter

import (
	"encoding/json"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// parseCodexRolloutLine decodes one JSONL line into an optional AgentEvent.
// Updates parser counters/session meta. Returns ok=false for non-emitted lines.
func parseCodexRolloutLine(p *rolloutParser, line []byte) (protocol.AgentEvent, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return protocol.AgentEvent{}, false
	}
	var typ string
	_ = json.Unmarshal(raw["type"], &typ)
	var payload map[string]any
	if pr, ok := raw["payload"]; ok {
		_ = json.Unmarshal(pr, &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	ts := time.Now().UTC()
	if traw, ok := raw["timestamp"]; ok {
		var tsStr string
		if json.Unmarshal(traw, &tsStr) == nil {
			if t, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
				ts = t
			} else if t, err := time.Parse(time.RFC3339, tsStr); err == nil {
				ts = t
			}
		}
	}

	switch typ {
	case "session_meta":
		if sid, ok := payload["session_id"].(string); ok && p.sessionID == "" {
			p.sessionID = sid
		}
		if p.sessionID == "" {
			if sid, ok := payload["id"].(string); ok {
				p.sessionID = sid
			}
		}
		if p.meta == nil {
			p.meta = map[string]string{}
		}
		if cwd, ok := payload["cwd"].(string); ok {
			p.meta["cwd"] = cwd
		}
		if o, ok := payload["originator"].(string); ok {
			p.meta["originator"] = o
		}
		return protocol.AgentEvent{}, false
	case "response_item":
		pt, _ := payload["type"].(string)
		switch pt {
		case "custom_tool_call", "function_call":
			p.toolCalls++
			name, _ := payload["name"].(string)
			if name == "exec" {
				p.execCalls++
			}
			if name == "spawn_agent" {
				p.spawnCalls++
			}
			p.seq++
			return makeToolEvent(p.sessionID, p.seq, ts, name, payload), true
		case "function_call_output", "custom_tool_call_output":
			out := payloadOutputString(payload["output"])
			if looksLikeFailure(out) {
				p.seq++
				return makeErrorEvent(p.sessionID, p.seq, ts, out), true
			}
		}
	}
	return protocol.AgentEvent{}, false
}
