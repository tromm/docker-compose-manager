#!/bin/bash
# Docker Compose Manager - Install Script
# Usage: curl -sSL https://raw.githubusercontent.com/tromm/docker-compose-manager/main/install.sh | sudo bash

set -e

REPO="tromm/docker-compose-manager"
INSTALL_DIR="/usr/local/bin"
CACHE_DIR="/var/cache/docker-compose-manager"
BINARY_NAME="docker-compose-manager"

# Detect OS and Architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
    x86_64)
        ARCH="amd64"
        ;;
    aarch64|arm64)
        ARCH="arm64"
        ;;
    *)
        echo "❌ Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

echo "🔍 Detected: ${OS}-${ARCH}"

# Get latest release
echo "📥 Downloading latest release..."
LATEST_URL=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep "browser_download_url.*${OS}-${ARCH}" | cut -d '"' -f 4)

if [ -z "$LATEST_URL" ]; then
    echo "❌ Could not find release for ${OS}-${ARCH}"
    exit 1
fi

# Download binary
TMP_FILE="/tmp/${BINARY_NAME}"
curl -L -o "$TMP_FILE" "$LATEST_URL"
chmod +x "$TMP_FILE"

# Install
echo "📦 Installing to ${INSTALL_DIR}..."
sudo mv "$TMP_FILE" "${INSTALL_DIR}/${BINARY_NAME}"

# Create cache directory
echo "📁 Creating cache directory..."
sudo mkdir -p "$CACHE_DIR"
sudo chmod 755 "$CACHE_DIR"

# Optional: Setup cron job
read -p "📅 Setup cron job for automatic update checks? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    CRON_CMD="0 */6 * * * ${INSTALL_DIR}/${BINARY_NAME} --update-cache"
    (crontab -l 2>/dev/null | grep -v "${BINARY_NAME}"; echo "$CRON_CMD") | crontab -
    echo "✅ Cron job added (runs every 6 hours)"
fi

echo ""
echo "✅ Installation complete!"
echo ""
echo "Usage:"
echo "  ${BINARY_NAME}              # Start TUI"
echo "  ${BINARY_NAME} --list       # List projects"
echo "  ${BINARY_NAME} --update-cache  # Check for updates"
echo ""
echo "Cache location: ${CACHE_DIR}/cache.json"
