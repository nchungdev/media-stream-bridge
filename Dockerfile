# Build stage
FROM golang:1.27-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS builder

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o media-stream-bridge ./cmd/server

# Final runtime image
FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf

# ffmpeg is the only runtime tool. FFmpeg < 8.1.2 is vulnerable to
# CVE-2026-8461 (PixelSmash), so the build fails if the repo ships less than 8.1.3.
RUN apk add --no-cache ca-certificates tzdata ffmpeg \
    && ffmpeg -version | head -1 \
    && v="$(ffmpeg -version | head -1 | awk '{print $3}')" \
    && [ "$(printf '%s\n8.1.3\n' "$v" | sort -V | head -1)" = "8.1.3" ] \
    || { echo "ffmpeg >= 8.1.3 required" >&2; exit 1; }

RUN adduser -D -u 1000 -G users app

COPY --from=builder /app/media-stream-bridge /usr/local/bin/media-stream-bridge

USER 1000:100

EXPOSE 6890

ENV MEDIA_STREAM_CONFIG=/config/config.json

ENTRYPOINT ["/usr/local/bin/media-stream-bridge"]
