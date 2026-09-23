BINARY := sre-agent
PKG    := ./cmd/sre-agent

.PHONY: build run dev test lint tidy clean web

build:
	go build -o bin/$(BINARY) $(PKG)

run: build
	./bin/$(BINARY)

# Runs the API and worker with the local Postgres and whatever model
# credential is in the environment. The UI is served only after `make web`;
# otherwise use `npm run dev` in web/, which proxies /v1 here.
dev:
	SRE_LOG_LEVEL=debug go run $(PKG)

test:
	go test ./...

lint:
	go vet ./...
	@command -v golangci-lint >/dev/null && golangci-lint run || echo "golangci-lint not installed, ran go vet only"

tidy:
	go mod tidy

# Builds the dashboard and copies it where go:embed picks it up, so the next
# `make build` serves the UI from the binary.
web:
	cd web && npm install && npm run build
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
	cp -R web/dist/. internal/webui/dist/

clean:
	rm -rf bin
