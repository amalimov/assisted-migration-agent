package analyzer

import (
	"embed"
	"io/fs"
)

//go:embed static
var embeddedAssets embed.FS

func analyzerAssets() fs.FS {
	assets, err := fs.Sub(embeddedAssets, "static")
	if err != nil {
		panic(err)
	}
	return assets
}
