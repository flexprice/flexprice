# Build stage
FROM golang:1.23-alpine AS builder
WORKDIR /app

ENV CGO_ENABLED=0 \
    GOOS=linux

# Pull deps from the module proxy. No vendor/ directory in the repo, so
# `go build -mod=mod` (the default) resolves from go.mod/go.sum.
COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./

RUN go build -ldflags="-w -s" -o integration-test .

# Final stage
FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/integration-test .

ENV TZ=UTC

CMD ["./integration-test"]
