# syntax=docker/dockerfile:1

# All-in-one image: UI + API on one port.
# For the API-only variant (no Node stage, no UI served), use Dockerfile.api.

# 1. Build the UI. The submodule pins the exact commit, so this stage is
#    reproducible and needs no network beyond npm.
FROM --platform=$BUILDPLATFORM node:26-alpine AS ui

WORKDIR /ui

COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci

COPY web/ ./
RUN npm run build

# 2. Build the API with the UI embedded. Stage order is load-bearing:
#    go:embed reads the filesystem at compile time, so the UI output must be
#    in internal/web/dist before `go build` runs.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine3.23 AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
COPY --from=ui /ui/dist ./internal/web/dist

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o floxflix ./cmd/server

# 3. Runtime. ffmpeg is the reason this stays on Alpine rather than distroless.
FROM alpine:3.23

RUN apk add --no-cache ffmpeg ca-certificates tzdata \
    && addgroup -S -g 10001 floxflix \
    && adduser -S -D -H -u 10001 -G floxflix floxflix

WORKDIR /app

COPY --from=builder /build/floxflix .

RUN mkdir -p /app/uploads && chown -R floxflix:floxflix /app

USER floxflix

ENV UPLOAD_FOLDER=/app/uploads
ENV PORT=7777

EXPOSE 7777

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
    CMD wget -qO- http://127.0.0.1:7777/healthz || exit 1

CMD ["/app/floxflix"]
