package webui

import (
	"errors"
	"io/fs"
	"strings"
	"testing/fstest"
)

// Deterministic build outputs shaped like a real `vite build`: an index that
// names fingerprinted bundles under assets/, plus files copied from public/.

const (
	fixtureIndex = `<!doctype html><html><head>` +
		`<script type="module" src="/assets/index-BYGcTU1T.js"></script>` +
		`<link rel="stylesheet" href="/assets/index-Bo2m9jmI.css"></head>` +
		`<body><div id="root"></div></body></html>`
	fixtureJS  = `console.log("sre-agent")`
	fixtureCSS = `body{margin:0}`
)

// fixtureBuild is a complete build.
func fixtureBuild() fstest.MapFS {
	return fstest.MapFS{
		"index.html":                {Data: []byte(fixtureIndex)},
		"assets/index-BYGcTU1T.js":  {Data: []byte(fixtureJS)},
		"assets/index-Bo2m9jmI.css": {Data: []byte(fixtureCSS)},
		"favicon.svg":               {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)},
		"agent/a1_01.png":           {Data: []byte{0x89, 'P', 'N', 'G'}},
		// Unicode file name, as a public/ asset could be.
		"agent/café-ü.png": {Data: []byte{0x89, 'P', 'N', 'G'}},
	}
}

// fixtureUnbuilt is what a checkout that never ran `make web` embeds.
func fixtureUnbuilt() fstest.MapFS {
	return fstest.MapFS{".gitkeep": {Data: nil}}
}

// fixtureEmpty has no entries at all.
func fixtureEmpty() fstest.MapFS { return fstest.MapFS{} }

// fixtureEmptyIndex has an index.html of zero bytes.
func fixtureEmptyIndex() fstest.MapFS {
	return fstest.MapFS{"index.html": {Data: []byte{}}}
}

// brokenFS fails every read with something other than "not exist", as a
// damaged or permission-denied filesystem would.
type brokenFS struct{}

var errBroken = errors.New("i/o timeout reading embedded asset")

func (brokenFS) Open(string) (fs.File, error) { return nil, errBroken }

// longRoute is a deep-link path at a generous length boundary.
var longRoute = "/runs/" + strings.Repeat("a", 4096)

// malformedPaths try to escape the root or confuse the cleaner. Each must
// end up either as a real file or as index.html, never outside fsys.
var malformedPaths = []string{
	"/../../etc/passwd",
	"/assets/../../index.html",
	"//assets//index-BYGcTU1T.js",
	"/./runs/./abc",
	"/%2e%2e/%2e%2e/etc/passwd",
}
