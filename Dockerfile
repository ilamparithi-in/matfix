### Build stage
FROM golang:1.26-bookworm AS builder

WORKDIR /src

# Cache module downloads separately from source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build both binaries. CGO is disabled - modernc.org/sqlite is pure Go.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -tags goolm \
      -o /out/matfix    ./cmd/matfix && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -tags goolm \
      -o /out/matfixctl ./cmd/matfixctl

### Runtime stage
FROM debian:bookworm-slim

# Install CA certificates (needed for TLS when talking to homeservers)
# and curl (used by the Docker healthcheck).
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*

# Non-root runtime user.
RUN groupadd -r matfix && useradd -r -g matfix -s /sbin/nologin matfix

# Persistent data volume - SQLite database lives here.
RUN install -d -o matfix -g matfix -m 0750 /data

# Admin socket directory - bind-mount or volume as needed.
RUN install -d -o matfix -g matfix -m 0750 /run/matfix

COPY --from=builder /out/matfix    /usr/local/bin/matfix
COPY --from=builder /out/matfixctl /usr/local/bin/matfixctl

USER matfix

# HTTP API
EXPOSE 8080
# Prometheus metrics
EXPOSE 9090

# Config file must be provided at /etc/matfix/config.yaml (bind mount or secret).
VOLUME ["/data", "/run/matfix"]

ENTRYPOINT ["/usr/local/bin/matfix"]
CMD ["--config", "/etc/matfix/config.yaml"]
