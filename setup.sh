#!/bin/bash
set -e

INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

echo "Building mailctl..."
go build -o "$INSTALL_DIR/mailctl" .

echo "Checking Apple Mail access..."
if ! osascript -e 'tell application "Mail" to return name of accounts' &>/dev/null 2>&1; then
    echo ""
    echo "Warning: Could not access Apple Mail."
    echo "Make sure Apple Mail is open and has at least one account configured."
    echo "You may need to grant Automation permissions:"
    echo "  System Settings → Privacy & Security → Automation → Terminal → Mail ✓"
    echo ""
fi

echo ""
echo "Done! mailctl is installed at $INSTALL_DIR/mailctl"
echo ""
echo "Next steps:"
echo "  mailctl sync   # import your inbox (takes ~10s)"
echo "  mailctl        # open TUI"
echo ""
echo "For AI integration, add to Claude Desktop config:"
echo '  { "mcpServers": { "mailctl": { "command": "mailctl", "args": ["mcp"] } } }'
