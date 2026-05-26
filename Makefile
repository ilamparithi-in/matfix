BINARY_DIR := bin
MATFIX     := $(BINARY_DIR)/matfix
MATFIXCTL  := $(BINARY_DIR)/matfixctl
VERSION    ?= $(shell sh -c 'tag=$$(git describe --tags --abbrev=0 --match "v[0-9]*.[0-9]*.[0-9]*" 2>/dev/null || true); if [ -n "$$tag" ]; then echo "$${tag#v}"; else echo 0.0.0; fi')
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE       ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# Use the pure-Go Olm implementation (goolm) to avoid CGO and the libolm C dep.
GOFLAGS    := -tags goolm
LDFLAGS    := -ldflags "-X github.com/ilamparithi-in/matfix/internal/version.Version=$(VERSION) -X github.com/ilamparithi-in/matfix/internal/version.Commit=$(COMMIT) -X github.com/ilamparithi-in/matfix/internal/version.Date=$(DATE)"

.PHONY: all build test lint run clean

all: build

build:
	@mkdir -p $(BINARY_DIR)
	go build $(GOFLAGS) $(LDFLAGS) -o $(MATFIX) ./cmd/matfix
	go build $(GOFLAGS) $(LDFLAGS) -o $(MATFIXCTL) ./cmd/matfixctl

test:
	go test $(GOFLAGS) ./...

lint:
	go vet $(GOFLAGS) ./...

run: build
	$(MATFIX)

clean:
	rm -rf $(BINARY_DIR)
