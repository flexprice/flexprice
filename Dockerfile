# syntax=docker/dockerfile:experimental
# Build stage
# Pin the builder to the runner's native arch ($BUILDPLATFORM) and
# cross-compile to the requested $TARGETARCH. Avoids QEMU emulation of
# the Go toolchain, which is 10-20x slower on multi-arch builds.
#
# Pinned to an exact patch: the image sets GOTOOLCHAIN=local, so the builder
# Go version must be >= the `go` directive in go.mod or `go mod download`
# hard-fails. Bump this whenever that directive moves.
FROM --platform=$BUILDPLATFORM golang:1.25.12-alpine AS builder
WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# Enterprise build. Empty by default, which produces the community image:
#
#   docker build -t flexprice .                            community
#   make docker-build-ee                                   enterprise
#
# ee/ is listed in .dockerignore, so `COPY . .` above never carries enterprise
# source into a community image, not even in an intermediate layer. The
# enterprise target swaps in an ignore file without that entry for the duration
# of the build, so ee/ is present only when it is deliberately requested.
ARG BUILD_TAGS=""

# Fail loudly rather than silently producing a community binary from an
# enterprise build request. ee/ reaches the context only when .dockerignore
# does not exclude it, which `make docker-build-ee` arranges.
RUN if [ "$BUILD_TAGS" = "ee" ] && [ ! -f ee/module.go ]; then \
        echo "BUILD_TAGS=ee but ee/ is not in the build context - use: make docker-build-ee"; exit 1; \
    fi

# TARGETARCH is provided automatically by buildx (e.g. amd64, arm64)
ARG TARGETARCH
ENV CGO_ENABLED=0 \
    GOOS=linux
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOARCH=$TARGETARCH go build -tags "$BUILD_TAGS" -ldflags="-w -s" -trimpath -o server ./cmd/server && \
    GOARCH=$TARGETARCH go build -ldflags="-w -s" -trimpath -o migrate ./cmd/migrate

# Typst stage
FROM ghcr.io/typst/typst:v0.13.1 AS typst

# Final stage
FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/server .
COPY --from=builder /app/migrate .
COPY --from=builder /app/migrations ./migrations
COPY --from=builder /app/internal/config ./config
COPY --from=builder /app/assets/fonts ./assets/fonts
COPY --from=builder /app/assets/typst-templates ./assets/typst-templates
COPY --from=builder /app/assets/email-templates ./assets/email-templates
COPY --from=typst /bin/typst /usr/local/bin/

ENV TZ=UTC

EXPOSE 8080
CMD ["./server"]