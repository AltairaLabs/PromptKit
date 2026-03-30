package web

import "embed"

//go:embed all:frontend/dist
var frontendFS embed.FS
