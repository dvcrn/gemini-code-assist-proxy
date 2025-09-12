#!/bin/bash

PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
PLIST_NAME="com.gemini-proxy.plist"
PLIST_LOCAL="${PROJECT_DIR}/${PLIST_NAME}"
PLIST_SYMLINK="${HOME}/Library/LaunchAgents/${PLIST_NAME}"

echo "Uninstalling gemini-proxy LaunchAgent..."

# Check if the service is running and unload it
if launchctl list | grep -q "com.gemini-proxy"; then
    echo "Stopping service..."
    launchctl unload "${PLIST_SYMLINK}" 2>/dev/null || true
    echo "Service stopped"
else
    echo "Service not currently running"
fi

# Remove the symlink
if [ -L "${PLIST_SYMLINK}" ]; then
    rm "${PLIST_SYMLINK}"
    echo "Removed symlink: ${PLIST_SYMLINK}"
elif [ -f "${PLIST_SYMLINK}" ]; then
    # If it's a regular file (old installation), remove it
    rm "${PLIST_SYMLINK}"
    echo "Removed plist file: ${PLIST_SYMLINK}"
else
    echo "Plist symlink/file not found: ${PLIST_SYMLINK}"
fi

# Optionally remove the local plist file
if [ -f "${PLIST_LOCAL}" ]; then
    read -p "Remove local plist file? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        rm "${PLIST_LOCAL}"
        echo "Removed local plist: ${PLIST_LOCAL}"
    else
        echo "Kept local plist: ${PLIST_LOCAL}"
    fi
fi

echo "✅ LaunchAgent uninstalled successfully"