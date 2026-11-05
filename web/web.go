// Package web embeds the browser UI.
package web

import "embed"

// FS holds the UI files.
//
//go:embed index.html app.js styles.css icon.svg
var FS embed.FS
