package codexruntime

import (
	"fmt"
	"strings"
)

// ProhibitedCredentialPaths are file paths and patterns that Reinframe is strictly forbidden from opening or parsing.
var ProhibitedCredentialPaths = []string{
	".codex/auth.json",
	".codex\\auth.json",
	"auth.json",
	".codex/tokens",
	".codex/session",
	".codex/credentials",
	"chatgpt_token",
	"oauth_token",
}

// ProhibitedSecretPatterns are token patterns that must never appear in snapshots, logs, arguments, or configs.
var ProhibitedSecretPatterns = []string{
	"sk-",
	"Bearer ",
	"refresh_token=",
	"refresh_token:",
	"access_token=",
	"oauth_token=",
	"client_secret=",
	"password=",
}

// AssertNoProhibitedPathAccess checks if a target path matches any prohibited credential store location.
func AssertNoProhibitedPathAccess(path string) error {
	normalized := strings.ToLower(filepathNormalize(path))
	for _, prohibited := range ProhibitedCredentialPaths {
		if strings.Contains(normalized, strings.ToLower(prohibited)) {
			return fmt.Errorf("security boundary violation: attempt to access prohibited credential store path %q", path)
		}
	}
	return nil
}

// AssertNoSecretLeakage scans arbitrary text (logs, errors, diagnostic strings, arguments) for raw secret leaks.
func AssertNoSecretLeakage(text string) error {
	for _, pattern := range ProhibitedSecretPatterns {
		if strings.Contains(text, pattern) {
			return fmt.Errorf("security violation: raw secret token pattern %q detected in output", pattern)
		}
	}
	// Also check for JWT format
	if strings.Contains(text, "eyJ") && len(text) > 30 {
		return fmt.Errorf("security violation: JWT token pattern detected in output")
	}
	return nil
}

func filepathNormalize(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}
