package config

import (
	"fmt"
	"regexp"
	"strings"
)

// entryPathPattern limits the secret panel entry prefix to one safe path
// segment: 1-64 characters of letters, digits, underscore or dash, starting
// with a letter or digit.
var entryPathPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// reservedEntryPaths are segments that keep a root-level meaning and therefore
// cannot become the entry prefix.
var reservedEntryPaths = map[string]bool{
	"api":       true,
	"__entry__": true,
}

// NormalizeEntryPath validates the secret entry prefix used by the panel entry
// gate. An empty result means the gate is disabled and the panel is reachable
// from the root as before.
func NormalizeEntryPath(raw string) (string, error) {
	trimmed := strings.Trim(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "", nil
	}
	if !entryPathPattern.MatchString(trimmed) {
		return "", fmt.Errorf("entry path must be 1-64 characters of letters, digits, '_' or '-', starting with a letter or digit")
	}
	if reservedEntryPaths[strings.ToLower(trimmed)] {
		return "", fmt.Errorf("entry path %q is reserved", trimmed)
	}
	return trimmed, nil
}

// EntryPathEnabled reports whether the secret entry gate is active.
func EntryPathEnabled() bool {
	return strings.Trim(AppConfig.EntryPath, "/") != ""
}
