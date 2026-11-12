package web

import (
	"embed"
	"io/fs"
)

// dist holds the built UI (Vite `dist/` output). The Docker build replaces the
// committed placeholder with the real build before `go build` runs, because
// go:embed reads the filesystem at compile time.
//
//go:embed all:dist
var dist embed.FS

// Dist returns the embedded UI build output rooted at the dist directory.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
