.PHONY: build install clean test release

BINARY_NAME=docker-compose-manager
INSTALL_DIR=/usr/local/bin
CACHE_DIR=/var/cache/docker-compose-manager

# Build for current platform
build:
	go build -ldflags="-s -w" -o $(BINARY_NAME) ./cmd

# Build for all platforms
build-all:
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o $(BINARY_NAME)-linux-amd64 ./cmd
	GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o $(BINARY_NAME)-linux-arm64 ./cmd
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o $(BINARY_NAME)-darwin-amd64 ./cmd
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o $(BINARY_NAME)-darwin-arm64 ./cmd

# Install locally
install: build
	sudo mkdir -p $(CACHE_DIR)
	sudo chmod 755 $(CACHE_DIR)
	sudo cp $(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "✅ Installed to $(INSTALL_DIR)/$(BINARY_NAME)"

# Uninstall
uninstall:
	sudo rm -f $(INSTALL_DIR)/$(BINARY_NAME)

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME)-*

# Run tests
test:
	go test -v ./...

# Format code
fmt:
	go fmt ./...

# Create release
release:
	@if [ -z "$(VERSION)" ]; then echo "Use: make release VERSION=v1.0.0"; exit 1; fi
	git tag -a $(VERSION) -m "Release $(VERSION)"
	git push origin $(VERSION)

help:
	@echo "make build         - Build binary"
	@echo "make install       - Install to system"
	@echo "make release VERSION=v1.0.0 - Create release"
