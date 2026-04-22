# Build stage
FROM golang:1.23-alpine AS builder
WORKDIR /app

ENV CGO_ENABLED=0 \
    GOOS=linux

COPY go.mod go.sum ./
COPY vendor/ ./vendor/
COPY *.go ./

RUN go build -mod=vendor -ldflags="-w -s" -o integration-test .

# Final stage
FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/integration-test .

ENV TZ=UTC

CMD ["./integration-test"]
