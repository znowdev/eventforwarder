#!/bin/bash

# Set the repository owner, repository name, and binary name
OWNER="znowdev"
REPO="reqbouncer"
BIN="reqbouncer"

# Get the OS and architecture
OS="$(uname | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

# Map the architecture to the correct value
if [ "$ARCH" = "x86_64" ]; then
    ARCH="amd64"
elif [ "$ARCH" = "aarch64" ]; then
    ARCH="arm64"
elif [ "$ARCH" = "arm64" ]; then
    ARCH="arm64"
else
    echo "Unsupported architecture: $ARCH"
    exit 1
fi

# Get the latest release tag
TAG=$(curl --silent "https://api.github.com/repos/$OWNER/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

# Set the download URL
URL="https://github.com/$OWNER/$REPO/releases/download/$TAG/$BIN_${OS}_$ARCH.tar.gz"

# Download the binary
curl -L $URL -o $BIN.tar.gz

# Extract the binary
tar -xzf $BIN.tar.gz

# Move the binary to /usr/local/bin
mv $BIN /usr/local/bin/

# Make the binary executable
chmod +x /usr/local/bin/$BIN

# Clean up
rm $BIN.tar.gz

echo "Installed $BIN ($TAG) for $OS/$ARCH"