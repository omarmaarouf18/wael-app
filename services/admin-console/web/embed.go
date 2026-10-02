// Package web embeds the admin console's static pages. The pages are plain
// HTML, CSS and ES modules: no framework, no build step, no CDN.
package web

import "embed"

// Files holds index.html, style.css and js/*.js. Tests live in web/test and
// are deliberately not embedded.
//
//go:embed index.html style.css js
var Files embed.FS
