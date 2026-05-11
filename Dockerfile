# syntax=docker/dockerfile:experimental
# Build stage
FROM golang:1.24-alpine3.20 AS builder
WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ENV CGO_ENABLED=0 \
    GOOS=linux
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-w -s" -trimpath -o server cmd/server/main.go

# Typst stage
FROM ghcr.io/typst/typst:v0.13.1 AS typst

# Final stage
FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/server .
COPY --from=builder /app/internal/config ./config
COPY --from=builder /app/assets/fonts ./assets/fonts
COPY --from=builder /app/assets/typst-templates ./assets/typst-templates
COPY --from=builder /app/assets/email-templates ./assets/email-templates
COPY --from=typst /bin/typst /usr/local/bin/

ENV TZ=UTC

EXPOSE 8080
CMD ["./server"]