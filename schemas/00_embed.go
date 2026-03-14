package schemas

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

const Suffix = ".amata.schema.json"

// Files contains the embedded built-in workflow schemas.
//
//go:embed *.amata.schema.json
var Files embed.FS

func Names() ([]string, error) {
	entries, err := fs.ReadDir(Files, ".")
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), Suffix) {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name(), Suffix))
	}
	sort.Strings(names)
	return names, nil
}

func Read(name string) ([]byte, error) {
	return Files.ReadFile(name + Suffix)
}
