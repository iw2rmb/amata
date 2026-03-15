package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

func ParseStructuredOutput(data []byte) (any, error) {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil, fmt.Errorf("structured output is empty")
	}

	candidates := []string{text}
	if stripped, ok := stripOuterFence(text); ok {
		candidates = append([]string{stripped}, candidates...)
	}

	for _, candidate := range candidates {
		if value, ok := decodeJSONPrefix(candidate); ok {
			return value, nil
		}
	}

	for _, candidate := range candidates {
		for index, r := range candidate {
			if !isJSONStartRune(r) {
				continue
			}
			if value, ok := decodeJSONPrefix(candidate[index:]); ok {
				return value, nil
			}
		}
	}

	return nil, fmt.Errorf("structured output does not contain valid JSON")
}

func StructuredPrompt(prompt string, schemaJSON string) string {
	var builder strings.Builder
	builder.WriteString(prompt)
	if !strings.HasSuffix(prompt, "\n") {
		builder.WriteByte('\n')
	}
	builder.WriteString("\nReturn only JSON that matches this schema.\n")
	builder.WriteString("Do not wrap the JSON in markdown fences.\n")
	builder.WriteString(schemaJSON)
	builder.WriteByte('\n')
	return builder.String()
}

func CommandEnv(overrides map[string]string) []string {
	base := map[string]string{}
	for _, entry := range os.Environ() {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		base[name] = value
	}

	for key, value := range overrides {
		base[key] = value
	}

	keys := make([]string, 0, len(base))
	for key := range base {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+base[key])
	}
	return env
}

func decodeJSONPrefix(text string) (any, bool) {
	decoder := json.NewDecoder(strings.NewReader(text))

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	return value, true
}

func stripOuterFence(text string) (string, bool) {
	if !strings.HasPrefix(text, "```") {
		return "", false
	}

	start := strings.IndexByte(text, '\n')
	if start < 0 {
		return "", false
	}
	if !strings.HasSuffix(text, "```") {
		return "", false
	}

	content := strings.TrimSuffix(text[start+1:], "```")
	return strings.TrimSpace(content), true
}

func isJSONStartRune(r rune) bool {
	switch {
	case r == '{', r == '[', r == '"', r == '-', r == 't', r == 'f', r == 'n':
		return true
	case r >= '0' && r <= '9':
		return true
	default:
		return false
	}
}
