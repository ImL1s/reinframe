package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

var (
	committedSchemaOnce sync.Once
	committedSchema     *jsonschema.Schema
	committedSchemaErr  error
)

// committedV2SchemaFSPath resolves docs/evidence/grok_build/reinframe.grok_build_live_control.v2.schema.json
// relative to this source file (works in tests and from repo checkout).
func committedV2SchemaFSPath() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	// cmd/groklive/schema_validate.go → repo root
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	p := filepath.Join(root, "docs", "evidence", "grok_build", "reinframe.grok_build_live_control.v2.schema.json")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("committed v2 schema: %w", err)
	}
	return p, nil
}

func loadCommittedV2Schema() (*jsonschema.Schema, error) {
	committedSchemaOnce.Do(func() {
		path, err := committedV2SchemaFSPath()
		if err != nil {
			committedSchemaErr = err
			return
		}
		b, err := os.ReadFile(path)
		if err != nil {
			committedSchemaErr = err
			return
		}
		c := jsonschema.NewCompiler()
		c.Draft = jsonschema.Draft2020
		url := "https://reinframe.dev/schemas/reinframe.grok_build_live_control.v2.json"
		if err := c.AddResource(url, strings.NewReader(string(b))); err != nil {
			committedSchemaErr = err
			return
		}
		sch, err := c.Compile(url)
		if err != nil {
			committedSchemaErr = err
			return
		}
		committedSchema = sch
	})
	return committedSchema, committedSchemaErr
}

// validateReportAgainstCommittedSchema validates the final report map against the
// repository's committed v2 JSON Schema artifact (not an in-memory subset).
func validateReportAgainstCommittedSchema(report map[string]any) error {
	sch, err := loadCommittedV2Schema()
	if err != nil {
		return err
	}
	// Round-trip through JSON so ScenarioResult structs become plain maps/arrays.
	raw, err := json.Marshal(report)
	if err != nil {
		return err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	if err := sch.Validate(doc); err != nil {
		return fmt.Errorf("schema validation: %w", err)
	}
	return nil
}
