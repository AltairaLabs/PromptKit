package web

import "embed"

//go:embed frontend/dist
var frontendFS embed.FS
