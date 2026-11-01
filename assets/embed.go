// Package assets embeds data files shipped inside the exe.
package assets

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
	"sync"
)

//go:embed dlc.dat.gz
var dlcGz []byte

// DLC returns v2fly domain-list-community's dlc.dat (see LICENSE-dlc),
// decompressed once on first use.
var DLC = sync.OnceValue(func() []byte {
	r, err := gzip.NewReader(bytes.NewReader(dlcGz))
	if err != nil {
		panic(err) // embedded at build time; a failure is a broken build
	}
	b, err := io.ReadAll(r)
	if err != nil {
		panic(err)
	}
	return b
})
