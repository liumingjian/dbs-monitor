//go:build !embed_web

package web

import (
	"embed"
	"io/fs"
)

//go:embed fallback
var files embed.FS

func Files() (fs.FS, error) {
	return fs.Sub(files, "fallback")
}
