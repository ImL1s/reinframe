// Command claudeinstall manages project-local Claude Code hooks for Reinframe (#106).
//
//	claudeinstall plan|install|uninstall|doctor -settings PATH -command BRIDGE_CMD
//
// Does not silently touch ~/.claude; operator supplies settings path.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	settings := fs.String("settings", "", "path to Claude settings.json (project-local recommended)")
	bridge := fs.String("command", "claudebridge pretool", "PreToolUse command to install")
	_ = fs.Parse(os.Args[2:])
	if *settings == "" {
		fmt.Fprintln(os.Stderr, "-settings required")
		os.Exit(2)
	}
	m := &adapter.ClaudeSettingsManager{SettingsPath: *settings, BridgeCommand: *bridge}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	switch cmd {
	case "plan":
		p, err := m.PlanInstall()
		if err != nil {
			fail(err)
		}
		_ = enc.Encode(p)
	case "install":
		if err := m.Install(); err != nil {
			fail(err)
		}
		fmt.Println(`{"ok":true,"action":"install"}`)
	case "uninstall":
		if err := m.Uninstall(); err != nil {
			fail(err)
		}
		fmt.Println(`{"ok":true,"action":"uninstall"}`)
	case "doctor":
		d, err := m.Doctor()
		if err != nil {
			fail(err)
		}
		_ = enc.Encode(d)
		if !d.OK {
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage: claudeinstall <plan|install|uninstall|doctor> -settings PATH [-command CMD]
`)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
