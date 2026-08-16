// Package web embeds the dependency-free bilingual web UI.
package web

import (
	"embed"
	"io/fs"
)

//go:embed dist/*
var assets embed.FS

func FS() fs.FS {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
