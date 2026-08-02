package protocol

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"unicode"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// MaxPayloadSize sets the upper limit (1MB) on event payloads to prevent DoS attacks.
const MaxPayloadSize = 1 * 1024 * 1024

//go:embed schemas/*.json
var embeddedSchemas embed.FS

var schemaCache map[string]*jsonschema.Schema

func init() {
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft7

	entries, err := fs.ReadDir(embeddedSchemas, "schemas")
	if err != nil {
		panic(fmt.Sprintf("failed to read embedded schemas directory: %v", err))
	}

	// Pass 1: Register all schemas as resources so cross-references ($ref) resolve cleanly
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		filePath := "schemas/" + entry.Name()
		data, err := embeddedSchemas.ReadFile(filePath)
		if err != nil {
			panic(fmt.Sprintf("failed to read embedded schema file %s: %v", entry.Name(), err))
		}

		url := "https://reinframe.dev/schemas/" + entry.Name()
		if err := compiler.AddResource(url, strings.NewReader(string(data))); err != nil {
			panic(fmt.Sprintf("failed to add schema resource %s: %v", url, err))
		}
	}

	// Pass 2: Compile schemas and cache them by snake_case type name
	cache := make(map[string]*jsonschema.Schema)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		url := "https://reinframe.dev/schemas/" + entry.Name()
		sch, err := compiler.Compile(url)
		if err != nil {
			panic(fmt.Sprintf("failed to compile schema %s: %v", entry.Name(), err))
		}

		typeName := strings.TrimSuffix(entry.Name(), ".json")
		cache[typeName] = sch
	}

	schemaCache = cache
}

// LoadSchemas returns nil since schemas are pre-compiled at package initialization (fail-fast).
func LoadSchemas() error {
	if schemaCache == nil {
		return fmt.Errorf("schema cache not initialized")
	}
	return nil
}

// ValidateEvent normalizes schemaType to snake_case and validates raw JSON payload against the corresponding compiled schema.
func ValidateEvent(payload []byte, schemaType string) error {
	if len(payload) > MaxPayloadSize {
		return fmt.Errorf("payload size %d exceeds maximum limit of %d bytes", len(payload), MaxPayloadSize)
	}

	normalized := toSnakeCase(schemaType)
	sch, ok := schemaCache[normalized]
	if !ok {
		return fmt.Errorf("unknown schema type: %q (normalized: %q)", schemaType, normalized)
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var v any
	if err := decoder.Decode(&v); err != nil {
		return fmt.Errorf("malformed JSON payload: %w", err)
	}

	if err := sch.Validate(v); err != nil {
		return fmt.Errorf("validation error for %q: %w", normalized, err)
	}

	return nil
}

// toSnakeCase converts PascalCase, camelCase, or UPPER_CASE strings to snake_case.
func toSnakeCase(s string) string {
	if strings.Contains(s, "_") {
		return strings.ToLower(s)
	}
	var buf strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if unicode.IsUpper(r) {
			if i > 0 {
				buf.WriteRune('_')
			}
			buf.WriteRune(unicode.ToLower(r))
		} else {
			buf.WriteRune(r)
		}
	}
	return buf.String()
}
