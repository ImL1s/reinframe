// Command codexctl is the operator surface for Codex JSONL observation (#107).
//
//	codexctl list|select|caps|doctor
//
// Observe-only; does not modify Codex files or claim tool gating.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	switch cmd {
	case "list":
		fs := flag.NewFlagSet("list", flag.ExitOnError)
		root := fs.String("root", defaultRoot(), "sessions root")
		maxAge := fs.Duration("max-age", 0, "optional max age filter")
		_ = fs.Parse(os.Args[2:])
		cands, err := adapter.DiscoverCodexRollouts(*root, *maxAge)
		if err != nil {
			fail(err)
		}
		_ = enc.Encode(cands)
	case "select":
		fs := flag.NewFlagSet("select", flag.ExitOnError)
		root := fs.String("root", defaultRoot(), "sessions root")
		path := fs.String("path", "", "explicit rollout path (required if multiple)")
		_ = fs.Parse(os.Args[2:])
		cands, err := adapter.DiscoverCodexRollouts(*root, 0)
		if err != nil {
			fail(err)
		}
		sel, err := adapter.SelectCodexRollout(cands, *path)
		if err != nil {
			fail(err)
		}
		_ = enc.Encode(sel)
	case "caps":
		_ = enc.Encode(adapter.DefaultCodexCapabilityManifest())
	case "doctor":
		fs := flag.NewFlagSet("doctor", flag.ExitOnError)
		path := fs.String("path", "", "rollout path")
		cursor := fs.String("cursor", "", "optional cursor json path")
		_ = fs.Parse(os.Args[2:])
		if *path == "" {
			fail(fmt.Errorf("-path required"))
		}
		st, err := os.Stat(*path)
		if err != nil {
			fail(err)
		}
		out := map[string]any{
			"path":   *path,
			"size":   st.Size(),
			"mod":    st.ModTime().UTC().Format(time.RFC3339),
			"caps":   adapter.DefaultCodexCapabilityManifest(),
			"writes": false,
		}
		if *cursor != "" {
			c, err := adapter.LoadCodexTailCursor(*cursor)
			if err != nil {
				fail(err)
			}
			c = adapter.ReconcileCursorAgainstFile(c, st.Size(), nil)
			out["cursor"] = c
		}
		_ = enc.Encode(out)
	default:
		usage()
		os.Exit(2)
	}
}

func defaultRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "sessions")
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  codexctl list [-root DIR] [-max-age DURATION]
  codexctl select [-root DIR] [-path ROLLOUT]
  codexctl caps
  codexctl doctor -path ROLLOUT [-cursor FILE]`)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
