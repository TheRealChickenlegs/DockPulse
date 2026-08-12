// Package web embeds the SvelteKit static bundle into the controller binary.
//
// The build pipeline (Makefile / Dockerfile) produces web/build and this
// package references that directory via go:embed. When the controller runs
// without an embedded bundle (typically during local development with a
// separately-served Vite dev server), it falls back to serving files from
// --web=<path>.
package web

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Content is the embedded SvelteKit build output.
//
// The path must match the directory produced by `npm run build` inside
// web/. A symbolic link or rename is acceptable at build time.
//
//go:embed all:build
var Content embed.FS

// Sub returns an fs.FS rooted at the bundle's top-level directory.
// It is used by net/http to serve the SPA.
func Sub() (fs.FS, error) {
	root, err := fs.Sub(Content, "build")
	if err != nil {
		return nil, err
	}
	// Sanity check: the embed must contain at least an index.html.
	if _, err := fs.Stat(root, "index.html"); err != nil {
		return nil, errors.New("embedded web bundle is missing index.html; did you run `npm run build` in web/?")
	}
	return root, nil
}

// OpenFromDisk loads the bundle from an explicit directory. Used during
// development when the embedded bundle is not available.
func OpenFromDisk(dir string) (fs.FS, error) {
	if dir == "" {
		return nil, errors.New("empty directory")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(abs, "index.html")); err != nil {
		return nil, errors.New("web bundle directory is missing index.html")
	}
	return os.DirFS(abs), nil
}