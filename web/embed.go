// Package web contains the self-contained GitChat browser application.
package web

import "embed"

// FS contains the static application assets.
//
//go:embed index.html app.js style.css platform.js platform.css models.js models.css sw.js manifest.webmanifest offline.html icons
var FS embed.FS
