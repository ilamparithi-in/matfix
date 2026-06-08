### Build stage
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS builder

WORKDIR /src

# Cache module downloads separately from source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=0.0.0
ARG COMMIT=none
ARG DATE=unknown

ARG TARGETOS
ARG TARGETARCH

ENV BUILD_LDFLAGS="-s -w -X github.com/ilamparithi-in/matfix/internal/version.Version=${VERSION} -X github.com/ilamparithi-in/matfix/internal/version.Commit=${COMMIT} -X github.com/ilamparithi-in/matfix/internal/version.Date=${DATE}"

# Build both binaries. CGO is disabled - modernc.org/sqlite is pure Go.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="${BUILD_LDFLAGS}" -tags goolm \
      -o /out/matfix    ./cmd/matfix && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="${BUILD_LDFLAGS}" -tags goolm \
      -o /out/matfixctl ./cmd/matfixctl

### Runtime stage
FROM debian:bookworm-slim

# Install CA certificates (needed for TLS when talking to homeservers)
# and curl (used by the Docker healthcheck).
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*

# Non-root runtime user - pinned UID/GID so volume ownership is stable across
# base image upgrades.
RUN groupadd -r -g 679 matfix && useradd -r -u 679 -g 679 -s /sbin/nologin matfix

# Persistent data volume - SQLite database lives here.
RUN install -d -o 679 -g 679 -m 0750 /data

# Admin socket directory.
RUN install -d -o 679 -g 679 -m 0750 /run/matfix

COPY --from=builder /out/matfix    /usr/local/bin/matfix
COPY --from=builder /out/matfixctl /usr/local/bin/matfixctl

USER 679:679

# HTTP API
EXPOSE 8080
# Prometheus metrics
EXPOSE 9090

# Config file must be provided at /etc/matfix/config.yaml (bind mount or secret).
VOLUME ["/data", "/run/matfix"]

ENTRYPOINT ["/usr/local/bin/matfix"]
CMD ["--config", "/etc/matfix/config.yaml"]
