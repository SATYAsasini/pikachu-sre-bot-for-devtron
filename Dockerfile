# One image carries the whole product: the API, the worker and the dashboard.
# Knowledge packs, prompts and the built UI are embedded in the binary, so the
# runtime image has no data directory and no configuration beyond environment.

# --- dashboard ---------------------------------------------------------------
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# --- binary ------------------------------------------------------------------
FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist/ ./internal/webui/dist/
RUN go build -trimpath -ldflags="-s -w" -o /out/sre-agent ./cmd/sre-agent

# --- runtime -----------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sre-agent /sre-agent
ENV SRE_HTTP_ADDR=:8090 \
    SRE_DOTENV=off \
    SRE_LOG_FORMAT=json
EXPOSE 8090
USER nonroot:nonroot
ENTRYPOINT ["/sre-agent"]
