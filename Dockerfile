# syntax=docker/dockerfile:experimental
# Build stage
# Pin the builder to the runner's native arch ($BUILDPLATFORM) and
# cross-compile to the requested $TARGETARCH. Avoids QEMU emulation of
# the Go toolchain, which is 10-20x slower on multi-arch builds.
#
# Pinned to an exact patch: the image sets GOTOOLCHAIN=local, so the builder
# Go version must be >= the `go` directive in go.mod or `go mod download`
# hard-fails. Bump this whenever that directive moves.
FROM --platform=$BUILDPLATFORM golang:1.25.13-alpine AS builder
WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# TARGETARCH is provided automatically by buildx (e.g. amd64, arm64)
ARG TARGETARCH
ENV CGO_ENABLED=0 \
    GOOS=linux
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOARCH=$TARGETARCH go build -ldflags="-w -s" -trimpath -o server ./cmd/server && \
    GOARCH=$TARGETARCH go build -ldflags="-w -s" -trimpath -o migrate ./cmd/migrate

# dbmate stage
#
# dbmate applies the reviewed .sql files in migrations/versioned/ and records
# them in schema_migrations. It ships as a BINARY rather than a Go dependency:
# its module carries drivers the application has no use for (gorm, BigQuery,
# MySQL, SQLite), and importing it would pull all of that into go.mod for every
# build. Built in its own throwaway module so the app's go.mod/go.sum are
# untouched, and pinned -- Go verifies the checksum against sum.golang.org.
FROM --platform=$BUILDPLATFORM golang:1.25.13-alpine AS dbmate
ARG TARGETARCH
ARG DBMATE_VERSION=v2.35.0
RUN apk add --no-cache git
WORKDIR /dbmate-build
ENV CGO_ENABLED=0 \
    GOOS=linux
RUN go mod init flexprice.local/dbmate-build && \
    go get github.com/amacneil/dbmate/v2@${DBMATE_VERSION} && \
    # dbmate transitively pulls older golang.org/x/net and golang.org/x/text
    # that Trivy flags as HIGH-severity CVEs (CVE-2026-46600, CVE-2026-56852).
    # Force the fixed minor versions in this throwaway module.
    go get golang.org/x/net@v0.56.0 golang.org/x/text@v0.39.0 && \
    GOARCH=$TARGETARCH go build -ldflags="-w -s" -trimpath \
      -o /out/dbmate github.com/amacneil/dbmate/v2

# Typst stage
FROM ghcr.io/typst/typst:v0.13.1 AS typst

# Final stage
FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata && \
    addgroup -S app && adduser -S -G app app

WORKDIR /app
COPY --from=builder /app/server .
COPY --from=builder /app/migrate .
COPY --from=builder /app/migrations ./migrations
COPY --from=builder /app/internal/config ./config
COPY --from=builder /app/assets/fonts ./assets/fonts
COPY --from=builder /app/assets/typst-templates ./assets/typst-templates
COPY --from=builder /app/assets/email-templates ./assets/email-templates
COPY --from=typst /bin/typst /usr/local/bin/
# `./migrate postgres up` execs this; keep it on PATH.
COPY --from=dbmate /out/dbmate /usr/local/bin/dbmate
RUN chown -R app:app /app

ENV TZ=UTC
USER app

EXPOSE 8080
CMD ["./server"]