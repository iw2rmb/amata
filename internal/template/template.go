package template

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type Evaluator func(expression string) (any, error)

func Render(text string, eval Evaluator) (any, error) {
	if !strings.Contains(text, "{{") {
		return text, nil
	}

	parts, err := parse(text)
	if err != nil {
		return nil, err
	}
	if len(parts) == 1 && parts[0].expression {
		return eval(parts[0].value)
	}

	var rendered strings.Builder
	for _, part := range parts {
		if !part.expression {
			rendered.WriteString(part.value)
			continue
		}

		value, err := eval(part.value)
		if err != nil {
			return nil, err
		}

		text, err := stringify(value)
		if err != nil {
			return nil, err
		}
		rendered.WriteString(text)
	}

	return rendered.String(), nil
}

type part struct {
	expression bool
	value      string
}

func parse(text string) ([]part, error) {
	parts := []part{}
	remaining := text
	for {
		start := strings.Index(remaining, "{{")
		if start < 0 {
			if remaining != "" {
				parts = append(parts, part{value: remaining})
			}
			return parts, nil
		}
		if start > 0 {
			parts = append(parts, part{value: remaining[:start]})
		}

		remaining = remaining[start+2:]
		end := strings.Index(remaining, "}}")
		if end < 0 {
			return nil, fmt.Errorf("unterminated template expression")
		}

		expression := strings.TrimSpace(remaining[:end])
		if expression == "" {
			return nil, fmt.Errorf("template expression must not be empty")
		}
		parts = append(parts, part{
			expression: true,
			value:      expression,
		})
		remaining = remaining[end+2:]
	}
}

func stringify(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "null", nil
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	}

	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", fmt.Errorf("render template value: %w", err)
	}

	return strings.TrimSuffix(buffer.String(), "\n"), nil
}
