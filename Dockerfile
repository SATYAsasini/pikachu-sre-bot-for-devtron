# One image carries the whole product: the API, the worker and the dashboard.
# Knowledge packs, prompts and the built UI are embedded in the binary, so the
# runtime image has no data directory and no configuration beyond environment.

# Both build stages are pinned to the *build* platform and cross-compile to the
# target. Left to itself, a multi-platform build runs each stage under QEMU for
# every non-native architecture, and the Go toolchain is not reliable there:
# `go mod download` on emulated arm64 dies partway through with an http2
# goroutine dump and exit code 2. Go cross-compiles natively with CGO off, so
# emulation buys nothing and costs the build.

# --- dashboard ---------------------------------------------------------------
# The output is static files, identical on every architecture, so this never
# needs to run anywhere but the host.
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# --- binary ------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
# Supplied by BuildKit for the platform being produced.
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist/ ./internal/webui/dist/
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/sre-agent ./cmd/sre-agent

# --- runtime -----------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sre-agent /sre-agent
ENV SRE_HTTP_ADDR=:8090 \
    SRE_DOTENV=off \
    SRE_LOG_FORMAT=json
EXPOSE 8090
USER nonroot:nonroot
ENTRYPOINT ["/sre-agent"]
