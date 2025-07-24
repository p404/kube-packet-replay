# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o kube-packet-replay .

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN adduser -D -u 1000 kube-packet-replay

# Copy binary from builder
COPY --from=builder /app/kube-packet-replay /usr/local/bin/kube-packet-replay

# Switch to non-root user
USER kube-packet-replay

# Set entrypoint
ENTRYPOINT ["kube-packet-replay"]