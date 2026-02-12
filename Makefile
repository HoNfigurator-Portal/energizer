# Energizer - HoN Game Server Manager & API
# Build targets for Windows and Linux

APP_NAME = energizer
VERSION = 1.0.0
BUILD_DIR = build
LDFLAGS = -ldflags="-s -w -X main.AppVersion=$(VERSION)"

.PHONY: all build build-windows build-linux clean deps test fmt vet

# Default: build for current platform
all: build

# Build for current platform
build:
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME) ./cmd/energizer

# Cross-compile for Windows (amd64)
build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-windows-amd64.exe ./cmd/energizer

# Cross-compile for Linux (amd64)
build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-linux-amd64 ./cmd/energizer

# Build both platforms
build-all: build-windows build-linux
	@echo "Build complete: $(BUILD_DIR)/"
	@ls -lh $(BUILD_DIR)/

# Download and verify dependencies
deps:
	go mod download
	go mod verify
	go mod tidy

# Run tests
test:
	go test -v -race -cover ./...

# Format code
fmt:
	gofmt -s -w .

# Run static analysis
vet:
	go vet ./...

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	go clean

# Run the application
run:
	go run ./cmd/energizer

# Generate self-signed TLS certificate for development
cert:
	mkdir -p config
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
		-keyout config/energizer-key.pem -out config/energizer-cert.pem \
		-days 365 -nodes -subj "/CN=energizer-local"
