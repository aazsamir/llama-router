#!/usr/bin/env bash
set -euo pipefail

go build .

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVICE_FILE="$SCRIPT_DIR/llama-router.service"
SYSTEMD_DIR="/etc/systemd/user"
SERVICE_NAME="llama-router"

TTL="5m"

echo "Installing $SERVICE_NAME..."

# Generate systemd unit file with substitutions
sudo tee "$SYSTEMD_DIR/$SERVICE_NAME.service" > /dev/null <<EOF
[Unit]
Description=llama-router - llama-server wrapper with TTL-based memory management
After=network.target

[Service]
Type=simple
User=$USER
WorkingDirectory=$SCRIPT_DIR
ExecStart=$SCRIPT_DIR/llama-router -preset $SCRIPT_DIR/preset.ini -ttl $TTL
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=llama-router

[Install]
WantedBy=default.target
EOF

echo "Reloading systemd..."
systemctl --user daemon-reload

echo "Enabling $SERVICE_NAME..."
systemctl enable --user "$SERVICE_NAME"

echo "Starting $SERVICE_NAME..."
systemctl start --user "$SERVICE_NAME"

echo ""
echo "Done! Status:"
systemctl status --user "$SERVICE_NAME" --no-pager