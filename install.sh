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

# Determine the real (non-root) user so cron and the interactive TUI share one cache.
RUN_USER="${SUDO_USER:-$(id -un)}"
CACHE_FILE="${CACHE_DIR}/cache.json"

# Create cache directory, owned by the user that will run both cron and the TUI.
echo "📁 Creating cache directory (owner: ${RUN_USER})..."
sudo mkdir -p "$CACHE_DIR"
sudo chown "${RUN_USER}:${RUN_USER}" "$CACHE_DIR"
sudo chmod 755 "$CACHE_DIR"

# Optional: Setup cron job (installed into RUN_USER's crontab, not root's).
read -p "📅 Setup cron job for automatic update checks? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    LOG_FILE="/home/${RUN_USER}/.cache/dcm-cron.log"
    # Explicit --cache guarantees cron and the TUI use the SAME file regardless of $HOME.
    CRON_CMD="0 */6 * * * ${INSTALL_DIR}/${BINARY_NAME} --update-cache --cache ${CACHE_FILE} /home/dockeruser/docker/ >> ${LOG_FILE} 2>&1"
    # Replace any previous entry for this binary; install into RUN_USER's crontab.
    (sudo -u "${RUN_USER}" crontab -l 2>/dev/null | grep -v "${BINARY_NAME}"; echo "$CRON_CMD") | sudo -u "${RUN_USER}" crontab -
    echo "✅ Cron job added for ${RUN_USER} (every 6h, logs to ${LOG_FILE})"
fi

echo ""
echo "✅ Installation complete!"
echo ""
echo "Usage:"
echo "  ${BINARY_NAME}                 # Start TUI"
echo "  ${BINARY_NAME} --list          # List projects"
echo "  ${BINARY_NAME} --update-cache  # Refresh available updates (cron)"
echo ""
echo "Cache location: ${CACHE_FILE}"
