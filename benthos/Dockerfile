# Multi-stage build for Benthos Flexprice
# Stage 1: Build the Go binary
FROM golang:1.22-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o benthos-flexprice main.go

# Stage 2: Create minimal runtime image
FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Create non-root user
RUN addgroup -g 1000 benthos && \
    adduser -D -u 1000 -G benthos benthos

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/benthos-flexprice /app/benthos-flexprice

# Copy example configurations
COPY --from=builder /build/examples /app/examples

# Change ownership
RUN chown -R benthos:benthos /app

# Switch to non-root user
USER benthos

# Expose Benthos HTTP server port
EXPOSE 4195

# Set entrypoint
ENTRYPOINT ["/app/benthos-flexprice"]

# Default command (can be overridden)
CMD ["-c", "/app/examples/generate.yaml"]

