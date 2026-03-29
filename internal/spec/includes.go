package spec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	errSpecIncludeCycle = errors.New("spec include cycle detected")
)

func compose(rootPath string) ([]byte, error) {
	rootNode, err := loadYAMLDocument(rootPath)
	if err != nil {
		return nil, err
	}

	cache := map[string]*yaml.Node{
		rootPath: rootNode,
	}
	if err := resolveIncludes(rootNode, rootPath, cache, []string{rootPath}); err != nil {
		return nil, err
	}

	data, err := yaml.Marshal(rootNode.Content[0])
	if err != nil {
		return nil, fmt.Errorf("encode composed spec %s: %w", rootPath, err)
	}
	return data, nil
}

func resolveIncludes(node *yaml.Node, sourcePath string, cache map[string]*yaml.Node, stack []string) error {
	if node == nil {
		return nil
	}

	if node.Kind == yaml.AliasNode && node.Alias != nil {
		return resolveIncludes(node.Alias, sourcePath, cache, stack)
	}

	if node.Kind == yaml.ScalarNode && node.Tag == "!include" {
		targetFile, pointer, err := parseIncludeRef(sourcePath, node.Value)
		if err != nil {
			return err
		}

		targetID := targetFile
		if pointer != "" {
			targetID = targetFile + "#" + pointer
		}
		if cycle := includeCycle(stack, targetID); len(cycle) > 0 {
			return fmt.Errorf("%w: %s", errSpecIncludeCycle, strings.Join(cycle, " -> "))
		}

		targetRoot, err := loadYAMLFromCache(targetFile, cache)
		if err != nil {
			return err
		}
		selected, err := selectNode(targetRoot.Content[0], pointer, targetFile)
		if err != nil {
			return err
		}

		replacement := cloneNode(selected)
		if err := resolveIncludes(replacement, targetFile, cache, append(stack, targetID)); err != nil {
			return err
		}
		replaceNode(node, replacement)
		return nil
	}

	for _, child := range node.Content {
		if err := resolveIncludes(child, sourcePath, cache, stack); err != nil {
			return err
		}
	}
	return nil
}

func loadYAMLFromCache(filePath string, cache map[string]*yaml.Node) (*yaml.Node, error) {
	if root, ok := cache[filePath]; ok {
		return root, nil
	}

	root, err := loadYAMLDocument(filePath)
	if err != nil {
		return nil, err
	}
	cache[filePath] = root
	return root, nil
}

func loadYAMLDocument(filePath string) (*yaml.Node, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read spec %s: %w", filePath, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("decode spec %s: %w", filePath, err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, fmt.Errorf("decode spec %s: document root must be present", filePath)
	}
	return &root, nil
}

func parseIncludeRef(sourcePath string, raw string) (string, string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", "", fmt.Errorf("decode spec %s: !include path must not be empty", sourcePath)
	}

	pathPart := value
	pointer := ""
	if hash := strings.Index(value, "#"); hash >= 0 {
		pathPart = strings.TrimSpace(value[:hash])
		pointer = strings.TrimSpace(value[hash+1:])
	}

	if pathPart == "" {
		return "", "", fmt.Errorf("decode spec %s: !include path must not be empty", sourcePath)
	}

	resolvedPath := pathPart
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(filepath.Dir(sourcePath), resolvedPath)
	}
	resolvedPath = filepath.Clean(resolvedPath)

	if pointer != "" && !strings.HasPrefix(pointer, "/") {
		return "", "", fmt.Errorf("decode spec %s: !include fragment must start with /", sourcePath)
	}
	return resolvedPath, pointer, nil
}

func selectNode(root *yaml.Node, pointer string, sourcePath string) (*yaml.Node, error) {
	if pointer == "" {
		return root, nil
	}

	current := root
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	for _, rawPart := range parts {
		part := decodePointerPart(rawPart)
		next, err := pointerStep(current, part)
		if err != nil {
			return nil, fmt.Errorf("decode spec %s: !include fragment %q: %w", sourcePath, pointer, err)
		}
		current = next
	}
	return current, nil
}

func pointerStep(node *yaml.Node, token string) (*yaml.Node, error) {
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind == yaml.ScalarNode && key.Value == token {
				return node.Content[i+1], nil
			}
		}
		return nil, fmt.Errorf("mapping key %q not found", token)
	case yaml.SequenceNode:
		index, err := strconv.Atoi(token)
		if err != nil {
			return nil, fmt.Errorf("sequence index %q is invalid", token)
		}
		if index < 0 || index >= len(node.Content) {
			return nil, fmt.Errorf("sequence index %d is out of range", index)
		}
		return node.Content[index], nil
	default:
		return nil, fmt.Errorf("cannot traverse node kind %d", node.Kind)
	}
}

func decodePointerPart(value string) string {
	replaced := strings.ReplaceAll(value, "~1", "/")
	return strings.ReplaceAll(replaced, "~0", "~")
}

func includeCycle(stack []string, target string) []string {
	index := -1
	for i := range stack {
		if stack[i] == target {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}

	cycle := append([]string{}, stack[index:]...)
	cycle = append(cycle, target)
	return cycle
}

func replaceNode(dst *yaml.Node, src *yaml.Node) {
	dst.Kind = src.Kind
	dst.Style = src.Style
	dst.Tag = src.Tag
	dst.Value = src.Value
	dst.Anchor = src.Anchor
	dst.Alias = src.Alias
	dst.Content = src.Content
	dst.HeadComment = src.HeadComment
	dst.LineComment = src.LineComment
	dst.FootComment = src.FootComment
}

func cloneNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}

	cloned := *node
	if len(node.Content) > 0 {
		cloned.Content = make([]*yaml.Node, len(node.Content))
		for i := range node.Content {
			cloned.Content[i] = cloneNode(node.Content[i])
		}
	}
	if node.Alias != nil {
		cloned.Alias = cloneNode(node.Alias)
	}
	return &cloned
}
