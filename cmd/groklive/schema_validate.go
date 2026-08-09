package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

//go:embed embed/reinframe.grok_build_live_control.v2.schema.json
var embeddedV2SchemaJSON []byte

var (
	committedSchemaOnce sync.Once
	committedSchema     *jsonschema.Schema
	committedSchemaErr  error
)

// EmbeddedV2SchemaJSON returns the build-time committed schema bytes (install-safe).
func EmbeddedV2SchemaJSON() []byte {
	out := make([]byte, len(embeddedV2SchemaJSON))
	copy(out, embeddedV2SchemaJSON)
	return out
}

// committedV2SchemaFSPath resolves the docs copy for drift tests (source checkout only).
func committedV2SchemaFSPath() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	p := filepath.Join(root, "docs", "evidence", "grok_build", "reinframe.grok_build_live_control.v2.schema.json")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("committed v2 schema: %w", err)
	}
	return p, nil
}

func loadCommittedV2Schema() (*jsonschema.Schema, error) {
	committedSchemaOnce.Do(func() {
		b := embeddedV2SchemaJSON
		if len(b) == 0 {
			committedSchemaErr = fmt.Errorf("embedded v2 schema empty")
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
// embedded committed v2 JSON Schema (works for go install / trimpath binaries).
func validateReportAgainstCommittedSchema(report map[string]any) error {
	sch, err := loadCommittedV2Schema()
	if err != nil {
		return err
	}
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
