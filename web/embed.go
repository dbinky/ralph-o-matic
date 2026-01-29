package web

import "embed"

//go:embed templates/*.html
var Templates embed.FS
