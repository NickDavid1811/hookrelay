# Stage 1: Build static binaries
FROM golang:1.27.2-alpine3.24@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS builder

WORKDIR /src

# Install certificates and tzdata for secure outbound TLS
RUN apk add --no-cache ca-certificates tzdata

# Leverage Docker cache for dependencies
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy application source
COPY cmd/ cmd/
COPY internal/ internal/

# Build statically linked binaries with stripped debug symbols
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /bin/dispatcher ./cmd/dispatcher && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /bin/receiver ./cmd/receiver

# Stage 2: Minimal runtime image
FROM alpine:3.24.0@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4

# Install minimal runtime requirements (curl for healthchecks, certs for HTTPS)
RUN apk add --no-cache ca-certificates tzdata curl

# Create unprivileged user and persistent data directory
RUN addgroup -S hookrelay && adduser -S hookrelay -G hookrelay -u 10001 && \
    mkdir -p /data && chown -R hookrelay:hookrelay /data

# Copy binaries from builder
COPY --from=builder /bin/dispatcher /usr/local/bin/dispatcher
COPY --from=builder /bin/receiver /usr/local/bin/receiver

# Include the project terms in the runtime image.
COPY LICENSE /usr/share/licenses/hookrelay/LICENSE

# Run as non-root
USER hookrelay:hookrelay

WORKDIR /data
VOLUME ["/data"]

EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://127.0.0.1:8080/readyz || exit 1

ENTRYPOINT ["/usr/local/bin/dispatcher"]
CMD ["--listen", "0.0.0.0:8080", "--insecure-allow-remote-listen", "--db", "/data/hookrelay.db"]
