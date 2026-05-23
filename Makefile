BINARY_DIR := bin
MATRIXMAN  := $(BINARY_DIR)/matrixman
MATFIXCTL  := $(BINARY_DIR)/matfixctl

.PHONY: all build test lint run clean

all: build

build:
	@mkdir -p $(BINARY_DIR)
	go build -o $(MATRIXMAN) ./cmd/matrixman
	go build -o $(MATFIXCTL) ./cmd/matfixctl

test:
	go test ./...

lint:
	go vet ./...

run: build
	$(MATRIXMAN)

clean:
	rm -rf $(BINARY_DIR)
