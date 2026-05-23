BINARY_DIR := bin
MATFIX     := $(BINARY_DIR)/matfix
MATFIXCTL  := $(BINARY_DIR)/matfixctl

.PHONY: all build test lint run clean

all: build

build:
	@mkdir -p $(BINARY_DIR)
	go build -o $(MATFIX) ./cmd/matfix
	go build -o $(MATFIXCTL) ./cmd/matfixctl

test:
	go test ./...

lint:
	go vet ./...

run: build
	$(MATFIX)

clean:
	rm -rf $(BINARY_DIR)
