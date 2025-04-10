# Makefile for clarifai-mcp-server-local

# Default target architecture (can be overridden)
GOOS ?= darwin
GOARCH ?= arm64

# Output binary path (relative to the project root)
OUTPUT_BINARY = ./mcp_binary

# Default target: build
all: build

# Build the main package
build:
	@echo "Building for $(GOOS)/$(GOARCH)..."
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(OUTPUT_BINARY) .
	@echo "Build successful! Binary created at $(OUTPUT_BINARY)"

test:
	@echo "Running tests..."
	@go test ./... -v
	@echo "All tests passed!"


# Clean the output binary
clean:
	@echo "Cleaning build artifacts..."
	@rm -f $(OUTPUT_BINARY)

.PHONY: all build clean
