package api

import (
	"net/http"
	"testing/fstest"

	"github.com/devtron-labs/devtron-sre-agent/internal/webui"
)

const fixtureUIIndex = `<!doctype html><div id="root"></div>`

// fixtureUI is a minimal built dashboard for router tests.
func fixtureUI() http.Handler {
	return webui.Handler(fstest.MapFS{
		"index.html":          {Data: []byte(fixtureUIIndex)},
		"assets/index-abc.js": {Data: []byte(`void 0`)},
	})
}

// fixtureServer has only what routing needs; handlers that reach into the
// other dependencies are not exercised by these tests.
func fixtureServer() *Server { return &Server{UI: fixtureUI()} }
