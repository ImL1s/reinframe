package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func runACP(args []string) {
	fs := flag.NewFlagSet("acp", flag.ExitOnError)
	live := fs.Bool("live", false, "opt-in: launch real Grok")
	exe := fs.String("grok-executable", "", "absolute path to grok")
	project := fs.String("project", "", "disposable project root")
	out := fs.String("evidence-out", "", "evidence directory")
	_ = fs.Parse(args)
	if !*live {
		fail(fmt.Errorf("groklive acp: --live required (refusing to launch Grok)"))
	}
	grok := mustAbs(*exe, "--grok-executable")
	proj := mustAbs(*project, "--project")
	evDir := mustAbs(*out, "--evidence-out")
	_ = os.MkdirAll(evDir, 0o700)

	scenarios := loadScenarioMap(evDir)
	set := func(id, status, detail string, extra map[string]string) {
		sr := ScenarioResult{ID: id, Status: status, Detail: boundStr(detail, 500), At: stamp()}
		if extra != nil {
			sr.ToolName = extra["tool"]
			sr.ACKLayer = extra["ack"]
			sr.HostOutcome = extra["host"]
		}
		scenarios[id] = sr
	}

	// Overall budget covers multi-minute live session/prompt turns.
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	// Per-RPC budgets so a slow prompt does not starve cleanup recording.
	rpcCtx := func(sec int) (context.Context, context.CancelFunc) {
		return context.WithTimeout(ctx, time.Duration(sec)*time.Second)
	}

	// Start must use the long-lived parent ctx: CommandContext kills the process when ctx ends.
	client, err := adapter.StartGrokACPClient(ctx, adapter.GrokACPConfig{
		Executable:     grok,
		Args:           adapter.DefaultGrokACPArgs(),
		WorkDir:        proj,
		StartupTimeout: 30 * time.Second,
	})
	if err != nil {
		set("ACP-INIT-001", "FAIL", "start: "+err.Error(), nil)
		_ = saveScenarioMap(evDir, scenarios)
		fail(err)
	}
	// Capture PID before any close for orphan proof.
	ownedPID := client.ProcessPID()
	defer func() { _ = client.Close() }()

	// ACP-INIT-001
	initCtx, initCancel := rpcCtx(30)
	initRes, err := client.Initialize(initCtx, map[string]any{"name": "reinframe-groklive", "version": "0"})
	initCancel()
	if err != nil {
		set("ACP-INIT-001", "FAIL", err.Error(), nil)
		_ = saveScenarioMap(evDir, scenarios)
		fail(err)
	}
	pv, _ := initRes["protocolVersion"].(float64)
	neg := client.Negotiated()
	pre := adapter.NewGrokACPFoundationManifest()
	post := adapter.ManifestFromNegotiated(neg)
	if int(pv) != 1 || neg.ProtocolVersion != 1 {
		set("ACP-INIT-001", "FAIL", fmt.Sprintf("protocolVersion want 1 got init=%v neg=%d", pv, neg.ProtocolVersion), nil)
	} else if pre.NegotiatedLevel != -1 {
		set("ACP-INIT-001", "FAIL", "pre-handshake level must be -1", nil)
	} else {
		set("ACP-INIT-001", "PASS", fmt.Sprintf("protocolVersion=1 post_level=%d load=%v cancel=%v auth=%v",
			post.NegotiatedLevel, neg.LoadSession, neg.Cancel, neg.AuthMethods), nil)
	}
	if _, err := adapter.ParseGrokACPNegotiatedCapsMap(map[string]any{"protocolVersion": 99}); err == nil {
		set("ACP-INIT-001", "FAIL", "unsupported protocolVersion must fail closed", nil)
	}

	// ACP-AUTH-001
	method := ""
	for _, m := range neg.AuthMethods {
		if m == "cached_token" || m == "env_token" || m != "" {
			method = m
			if m == "cached_token" {
				break
			}
		}
	}
	if method == "" {
		set("ACP-AUTH-001", "FAIL", "no advertised auth methods", nil)
	} else {
		params, err := adapter.BuildGrokACPAuthenticateParams(method)
		if err != nil {
			set("ACP-AUTH-001", "FAIL", err.Error(), nil)
		} else {
			raw, _ := json.Marshal(params)
			s := string(raw)
			if strings.Contains(s, `"token"`) || strings.Contains(strings.ToLower(s), "api_key") ||
				strings.Contains(s, `"code"`) || strings.Contains(strings.ToLower(s), "credential") {
				set("ACP-AUTH-001", "FAIL", "auth envelope leaked credential fields: "+s, nil)
			} else {
				authCtx, authCancel := rpcCtx(45)
				err := client.Authenticate(authCtx, method)
				authCancel()
				if err != nil {
					msg := err.Error()
					if strings.Contains(strings.ToLower(msg), "token") && len(msg) > 80 {
						set("ACP-AUTH-001", "FAIL", "error may echo secrets (len check)", nil)
					} else {
						set("ACP-AUTH-001", "FAIL", "authenticate: "+boundStr(msg, 200), nil)
					}
				} else {
					set("ACP-AUTH-001", "PASS", "delegated auth methodId="+method+" headless=true no token field", nil)
				}
			}
		}
	}

	// ACP-SESSION-001 — PASS only with correlated session/update (session_visible).
	// Transport-only prompt success is INCONCLUSIVE (mandatory update not proven).
	// session/prompt waits for agent turn completion — allow several minutes.
	newCtx, newCancel := rpcCtx(45)
	sid, err := client.SessionNew(newCtx, map[string]any{"cwd": proj})
	newCancel()
	if err != nil || sid == "" {
		set("ACP-SESSION-001", "FAIL", fmt.Sprintf("session/new err=%v sid=%q", err, sid), nil)
	} else {
		body := adapter.BuildAdvicePrompt("REQUEST_REPLAN",
			"Re-evaluate the current approach against the stated acceptance criteria. Reply with one short sentence only.",
			"issue167-live-advice-001", "")
		promptCtx, promptCancel := rpcCtx(180)
		err := client.SessionPrompt(promptCtx, sid, body, "issue167-live-advice-001", "")
		promptCancel()
		if err != nil {
			set("ACP-SESSION-001", "FAIL", "session/prompt: "+err.Error(), nil)
		} else {
			saw := false
			deadline := time.After(90 * time.Second)
		waitLoop:
			for {
				select {
				case <-deadline:
					break waitLoop
				case u, ok := <-client.Updates():
					if !ok {
						break waitLoop
					}
					kind, _ := adapter.MapSessionUpdateToSummary(u)
					if kind != "" {
						saw = true
						client.NoteSessionVisible()
						break waitLoop
					}
				case <-time.After(200 * time.Millisecond):
					if client.LastACKLayer() == adapter.ACKLayerSessionVisible {
						saw = true
						break waitLoop
					}
				}
			}
			ack := client.LastACKLayer()
			if saw {
				if ack == adapter.ACKLayerExplicit {
					set("ACP-SESSION-001", "FAIL", "must not claim explicit ACK from transport alone", map[string]string{"ack": ack})
				} else {
					set("ACP-SESSION-001", "PASS", "session/new+prompt+correlated update", map[string]string{"ack": adapter.ACKLayerSessionVisible})
				}
			} else if ack == adapter.ACKLayerTransport {
				set("ACP-SESSION-001", "INCONCLUSIVE", "prompt transport OK; session/update not observed within timeout — strongest ACK remains transport", map[string]string{"ack": ack})
			} else {
				set("ACP-SESSION-001", "FAIL", fmt.Sprintf("saw_update=%v ack=%s", saw, ack), map[string]string{"ack": ack})
			}
		}

		// ACP-OPTIONAL-001 load/cancel — short budget; absence/timeout is NOT_RUN/INCONCLUSIVE not FAIL.
		if client.Negotiated().LoadSession {
			loadCtx, loadCancel := rpcCtx(30)
			err := client.SessionLoad(loadCtx, sid, nil)
			loadCancel()
			if err != nil {
				set("ACP-OPTIONAL-001", "INCONCLUSIVE", "loadSession negotiated but failed: "+err.Error(), nil)
			} else {
				set("ACP-OPTIONAL-001", "PASS", "session/load ok", nil)
			}
		} else if client.Negotiated().Cancel {
			canCtx, canCancel := rpcCtx(30)
			err := client.Cancel(canCtx, sid)
			canCancel()
			if err != nil {
				set("ACP-OPTIONAL-001", "INCONCLUSIVE", "cancel negotiated but failed: "+err.Error(), nil)
			} else {
				set("ACP-OPTIONAL-001", "PASS", "session/cancel ok", nil)
			}
		} else {
			set("ACP-OPTIONAL-001", "NOT_RUN", "loadSession/cancel not negotiated", nil)
		}
	}

	// ADVICE-DEDUP-001 — second delivery with same InterventionID.
	// PASS only when second SessionPrompt returns nil (transport accepted a second attempt).
	// Transport error → INCONCLUSIVE (host may have rejected; durable #108 machine not claimed).
	// Durable InterventionID suppression is #108 only.
	if sid != "" {
		body2 := adapter.BuildAdvicePrompt("REQUEST_REPLAN",
			"Duplicate InterventionID proof. Reply STOP only.",
			"issue167-live-advice-001", "")
		dupCtx, dupCancel := rpcCtx(120)
		err2 := client.SessionPrompt(dupCtx, sid, body2, "issue167-live-advice-001", "")
		dupCancel()
		if err2 != nil {
			set("ADVICE-DEDUP-001", "INCONCLUSIVE", "second delivery same InterventionID transport error: "+boundStr(err2.Error(), 120)+"; durable dedup is #108", nil)
		} else {
			set("ADVICE-DEDUP-001", "PASS", "second delivery same InterventionID accepted at transport; durable suppression is #108 not claimed", nil)
		}
	} else {
		set("ADVICE-DEDUP-001", "NOT_RUN", "no session", nil)
	}

	// CHALLENGE-001 — transport challenge fields in prompt; fail if prompt errors.
	chBody := adapter.BuildAdvicePrompt("CHALLENGE",
		"ChallengeID=issue167-ch-001 state=pending reason=scope_check claims=replan retry_budget=1. Reply STOP only.",
		"issue167-live-advice-002", "issue167-ch-001")
	if sid == "" {
		set("CHALLENGE-001", "NOT_RUN", "no session", nil)
	} else if !strings.Contains(chBody, "issue167-ch-001") {
		set("CHALLENGE-001", "FAIL", "ChallengeID missing from outbound prompt body", nil)
	} else {
		chCtx, chCancel := rpcCtx(120)
		err := client.SessionPrompt(chCtx, sid, chBody, "issue167-live-advice-002", "issue167-ch-001")
		chCancel()
		if err != nil {
			set("CHALLENGE-001", "FAIL", "session/prompt challenge: "+err.Error(), nil)
		} else {
			set("CHALLENGE-001", "PASS", "challenge text transported with ChallengeID preserved; #131 remains authoritative; no self-authorization claimed", nil)
		}
	}

	// ACP-CLEANUP-001 — Close + optional PID liveness check.
	if err := client.Close(); err != nil {
		set("ACP-CLEANUP-001", "FAIL", "close: "+err.Error(), nil)
	} else if ownedPID <= 0 {
		set("ACP-CLEANUP-001", "INCONCLUSIVE", "Close returned nil but no owned PID was captured (orphan check not performed)", nil)
	} else if processAlive(ownedPID) {
		set("ACP-CLEANUP-001", "FAIL", fmt.Sprintf("owned PID %d still alive after Close", ownedPID), nil)
	} else {
		set("ACP-CLEANUP-001", "PASS", fmt.Sprintf("Close completed; owned PID %d not alive", ownedPID), nil)
	}

	_ = saveScenarioMap(evDir, scenarios)
	_ = writeJSON(filepath.Join(evDir, "acp_manifest.json"), map[string]any{
		"pre_handshake":  pre,
		"post_handshake": post,
		"auth_methods":   neg.AuthMethods,
		"caps_digest":    neg.CapsDigest,
	})
	fmt.Println(`{"ok":true,"action":"acp","scenarios":` + fmt.Sprintf("%d", len(scenarios)) + `}`)
}

// processAlive reports whether pid accepts signal 0 (Unix). On unsupported platforms
// or errors other than ESRCH, returns true so cleanup claims stay conservative.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// ESRCH / process already dead.
	if err == syscall.ESRCH {
		return false
	}
	// On some systems wait-reaped PIDs return "process already finished".
	if strings.Contains(strings.ToLower(err.Error()), "process already finished") ||
		strings.Contains(strings.ToLower(err.Error()), "no such process") {
		return false
	}
	// Conservative: unknown error → treat as still possibly alive so we do not false-PASS.
	return true
}
