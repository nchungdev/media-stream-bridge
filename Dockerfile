# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o media-stream-bridge ./cmd/server

# Final runtime image
FROM alpine:3.20

# Install runtime dependencies for streaming and downloading
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    ffmpeg \
    yt-dlp \
    curl

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/media-stream-bridge /usr/local/bin/media-stream-bridge

# Create standard directories
RUN mkdir -p /config /downloads /media

EXPOSE 6890

ENV MEDIA_STREAM_CONFIG=/config/config.json

ENTRYPOINT ["/usr/local/bin/media-stream-bridge"]
