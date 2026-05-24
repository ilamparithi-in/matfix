BINARY_DIR := bin
MATFIX     := $(BINARY_DIR)/matfix
MATFIXCTL  := $(BINARY_DIR)/matfixctl

# Use the pure-Go Olm implementation (goolm) to avoid CGO and the libolm C dep.
GOFLAGS    := -tags goolm

.PHONY: all build test lint run clean

all: build

build:
	@mkdir -p $(BINARY_DIR)
	go build $(GOFLAGS) -o $(MATFIX) ./cmd/matfix
	go build $(GOFLAGS) -o $(MATFIXCTL) ./cmd/matfixctl

test:
	go test $(GOFLAGS) ./...

lint:
	go vet $(GOFLAGS) ./...

run: build
	$(MATFIX)

clean:
	rm -rf $(BINARY_DIR)
