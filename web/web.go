// Package web embeds the Workbench frontend assets into the binary.
//
// Per docs/WORKBENCH.md §2 principle 1, everything — templates, htmx, CSS,
// canvas JS — is baked into the binary via embed.FS. No npm, no toolchain,
// no CDN.
package web

import "embed"

// Templates holds the html/template sources.
//
//go:embed templates/*.html
var Templates embed.FS

// Static holds the embedded static assets (CSS, JS).
//
//go:embed static
var Static embed.FS
