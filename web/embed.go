// Package web holds the frontend assets embedded into the nameworthy binary.
package web

import "embed"

// FS contains the page templates and local CSS and JS.
//
//go:embed templates static
var FS embed.FS
