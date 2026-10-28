package amata

import (
	_ "embed"
	"strings"
)

// Embed the release authority so source and release builds report the same version.
//
//go:embed VERSION
var versionText string

func Version() string { return strings.TrimSpace(versionText) }
