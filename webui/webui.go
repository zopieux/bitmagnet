package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

func StaticFS() fs.FS {
	return distFS
}
