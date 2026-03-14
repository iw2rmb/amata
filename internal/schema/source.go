package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ResolveResponseSchemaPath(rawSchema any, specPath string) (string, bool, error) {
	text, ok := rawSchema.(string)
	if !ok {
		return "", false, nil
	}
	if !looksLikeSchemaPath(text) {
		return "", false, nil
	}

	path := text
	if !filepath.IsAbs(path) {
		if specPath == "" {
			return "", false, fmt.Errorf("response schema path %q requires spec path", text)
		}
		path = filepath.Join(filepath.Dir(specPath), path)
	}
	return filepath.Clean(path), true, nil
}

func LoadResponseSchemaFile(path string) (any, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read schema file %s: %w", path, err)
	}

	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, "", fmt.Errorf("decode schema file %s: %w", path, err)
	}
	return document, string(data), nil
}

func looksLikeSchemaPath(text string) bool {
	switch text {
	case "", "string", "number", "boolean", "object":
		return false
	}
	if strings.HasPrefix(text, "#/") {
		return false
	}
	if filepath.IsAbs(text) {
		return true
	}
	if strings.HasPrefix(text, "./") || strings.HasPrefix(text, "../") {
		return true
	}
	if strings.HasSuffix(text, ".json") {
		return true
	}
	return strings.ContainsRune(text, '/')
}
