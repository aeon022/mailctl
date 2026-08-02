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

# ── MCP: register in ~/.claude.json ───────────────────────────────────────────
CLAUDE_JSON="$HOME/.claude.json"
if command -v python3 &>/dev/null; then
  python3 - "$CLAUDE_JSON" "$INSTALL_DIR/mailctl" <<'PYEOF'
import json, sys, os

claude_json = sys.argv[1]
binary_path = sys.argv[2]

data = {}
if os.path.exists(claude_json):
    with open(claude_json) as f:
        try:
            data = json.load(f)
        except Exception:
            pass

data.setdefault("mcpServers", {})
data["mcpServers"]["mailctl"] = {
    "command": binary_path,
    "args": ["mcp"]
}

with open(claude_json, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")

print("✓ MCP server registered in ~/.claude.json")
print("  Restart Claude Code to activate mailctl MCP tools")
PYEOF
else
  echo ""
  echo "  To enable MCP (Claude Code integration), add to ~/.claude.json:"
  echo "  \"mcpServers\": { \"mailctl\": { \"command\": \"$INSTALL_DIR/mailctl\", \"args\": [\"mcp\"] } }"
fi

echo ""
echo "Next steps:"
echo "  mailctl sync   # import your inbox (takes ~10s)"
echo "  mailctl        # open TUI"
