package protocol

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"unicode"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

//go:embed schemas/*.json
var embeddedSchemas embed.FS

var (
	schemaCache map[string]*jsonschema.Schema
	schemaOnce  sync.Once
	schemaErr   error
)

// LoadSchemas compiles and caches all embedded JSON schemas into memory.
func LoadSchemas() error {
	schemaOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		compiler.Draft = jsonschema.Draft7

		entries, err := fs.ReadDir(embeddedSchemas, "schemas")
		if err != nil {
			schemaErr = fmt.Errorf("failed to read embedded schemas directory: %w", err)
			return
		}

		// Pass 1: Register all schemas as resources so cross-references ($ref) resolve cleanly
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}

			filePath := "schemas/" + entry.Name()
			data, err := embeddedSchemas.ReadFile(filePath)
			if err != nil {
				schemaErr = fmt.Errorf("failed to read embedded schema file %s: %w", entry.Name(), err)
				return
			}

			url := "https://reinframe.dev/schemas/" + entry.Name()
			if err := compiler.AddResource(url, strings.NewReader(string(data))); err != nil {
				schemaErr = fmt.Errorf("failed to add schema resource %s: %w", url, err)
				return
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
				schemaErr = fmt.Errorf("failed to compile schema %s: %w", entry.Name(), err)
				return
			}

			typeName := strings.TrimSuffix(entry.Name(), ".json")
			cache[typeName] = sch
		}

		schemaCache = cache
	})

	return schemaErr
}

// ValidateEvent normalizes schemaType to snake_case and validates raw JSON payload against the corresponding compiled schema.
func ValidateEvent(payload []byte, schemaType string) error {
	if err := LoadSchemas(); err != nil {
		return fmt.Errorf("failed to initialize schema validator: %w", err)
	}

	normalized := toSnakeCase(schemaType)
	sch, ok := schemaCache[normalized]
	if !ok {
		return fmt.Errorf("unknown schema type: %q (normalized: %q)", schemaType, normalized)
	}

	var v any
	if err := json.Unmarshal(payload, &v); err != nil {
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
