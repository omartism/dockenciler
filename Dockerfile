# Build stage
FROM golang:1.26-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Copy source code
COPY . .

# Build the application
# -ldflags="-s -w" strips symbol table and debug information to reduce binary size
# CGO_ENABLED=0 ensures a statically linked binary for scratch/distroless images
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o dockenciler ./cmd/dockenciler

# Final stage
FROM gcr.io/distroless/static-debian13:latest

# Set working directory
WORKDIR /

# Copy binary from builder
COPY --from=builder /app/dockenciler /dockenciler

# Default environment variables
ENV LOG_LEVEL=info \
    RECONCILE_INTERVAL=5m \
    DOCKER_SOCKET_PATH=/var/run/docker.sock \
    DOCKER_LABEL_FILTER=dockenciler.autoupdate=true \
    DRY_RUN=false

# Healthcheck to ensure the process is running
HEALTHCHECK --interval=30s --timeout=3s \
  CMD ps aux | grep dockenciler || exit 1

# Entrypoint
ENTRYPOINT ["/dockenciler"]
