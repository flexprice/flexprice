# syntax=docker/dockerfile:experimental
# Build stage
# Pin the builder to the runner's native arch ($BUILDPLATFORM) and
# cross-compile to the requested $TARGETARCH. Avoids QEMU emulation of
# the Go toolchain, which is 10-20x slower on multi-arch builds.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine3.22 AS builder
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
    GOARCH=$TARGETARCH go build -ldflags="-w -s" -trimpath -o migrate ./cmd/migrate && \
    GOARCH=$TARGETARCH go build -ldflags="-w -s" -trimpath -o kafka-migrate cmd/kafka-migrate && \
    GOARCH=$TARGETARCH go build -ldflags="-w -s" -trimpath -o svix-migrate ./cmd/svix-migrate

# Typst stage
FROM ghcr.io/typst/typst:v0.13.1 AS typst

# Final stage
FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/server .
COPY --from=builder /app/migrate .
COPY --from=builder /app/svix-migrate .
COPY --from=builder /app/kafka-migrate .
COPY --from=builder /app/topics.yaml .
COPY --from=builder /app/migrations ./migrations
COPY --from=builder /app/internal/config ./config
COPY --from=builder /app/assets/fonts ./assets/fonts
COPY --from=builder /app/assets/typst-templates ./assets/typst-templates
COPY --from=builder /app/assets/email-templates ./assets/email-templates
COPY --from=typst /bin/typst /usr/local/bin/

ENV TZ=UTC

EXPOSE 8080
CMD ["./server"]