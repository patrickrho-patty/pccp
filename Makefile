.PHONY: all build build-go build-web clean dev dev-server dev-relay dev-pia test test-go test-e2e demo

BINARY_DIR := bin
WEB_DIR := web

all: build

# Build everything
build: build-go build-web

# Build Go binaries
build-go:
	mkdir -p $(BINARY_DIR)
	go build -o $(BINARY_DIR)/pccp-server ./cmd/pccp-server/
	go build -o $(BINARY_DIR)/pccp-relay ./cmd/pccp-relay/
	go build -o $(BINARY_DIR)/pccp-pia ./cmd/pccp-pia/

# Build React frontend
build-web:
	cd $(WEB_DIR) && pnpm install && pnpm build

# Run Go tests
test-go:
	go test ./internal/... -v

# Run all tests
test: test-go

# Clean build artifacts
clean:
	rm -rf $(BINARY_DIR) $(WEB_DIR)/dist .data/pccp.db

# Development servers
dev: dev-server

# Start Control Plane server
dev-server:
	go run ./cmd/pccp-server/

# Start Relay
dev-relay:
	go run ./cmd/pccp-relay/

# Start PIA (mock serving engine)
dev-pia:
	PCCP_PIA_ENGINE=mock go run ./cmd/pccp-pia/

# Start PIA with enrollment
dev-pia-enroll: dev-pia
	PCCP_PIA_ENGINE=mock go run ./cmd/pccp-pia/ --enroll

# Format code
fmt:
	go fmt ./...

# Vet code
vet:
	go vet ./...

# Tidy modules
tidy:
	go mod tidy
