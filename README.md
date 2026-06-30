# mailctl

Terminal email client for Apple Mail. Syncs your inbox to SQLite, provides a fast TUI and an MCP server for AI agents.

## Features

- Full-screen TUI: account tabs, date group headers, body preview, sender colors
- Sync from Apple Mail via AppleScript (IMAP + Exchange/Office365)
- Compose, reply with quoted text, save drafts
- Mark as read/unread, delete (moves to Trash in Apple Mail)
- Open any message in Apple Mail with `o`
- MCP server: exposes inbox, search, send, thread tools to AI agents
- SQLite store with WAL mode — fast even at thousands of messages

## Requirements

- macOS with Apple Mail configured and at least one account
- Go 1.21+

## Setup

```bash
git clone https://github.com/aeon022/mailctl
cd mailctl
./setup.sh
```

Or manually:

```bash
go build -o ~/.local/bin/mailctl .
mailctl sync          # initial sync from Apple Mail
mailctl               # open TUI
```

## TUI Keybindings

### List view
| Key | Action |
|-----|--------|
| `enter` | Open message |
| `n` | Compose new message |
| `s` | Sync inbox from Apple Mail |
| `u` | Toggle unread-only filter |
| `d` | Delete message (moves to Trash) |
| `o` | Open in Apple Mail |
| `/` | Search (subject, from, body) |
| `tab` / `shift+tab` | Switch account |
| `j` / `k` or `↓` / `↑` | Navigate |
| `PgDn` / `PgUp` | Page down / up |
| `g` / `G` | First / last message |
| `q` | Quit |

### Detail view
| Key | Action |
|-----|--------|
| `esc` | Back to list |
| `r` | Reply (opens compose with quoted text) |
| `u` | Mark as unread |
| `d` | Delete message |
| `o` | Open in Apple Mail |
| `↑` / `↓` | Scroll body |

### Compose view
| Key | Action |
|-----|--------|
| `tab` / `shift+tab` | Next / previous field |
| `ctrl+s` | Send |
| `ctrl+d` | Save to Drafts |
| `esc` | Cancel |

## CLI Commands

```bash
mailctl               # open TUI (default)
mailctl tui           # open TUI
mailctl sync          # sync inbox from Apple Mail
mailctl inbox         # list messages (JSON with --json)
mailctl send msg.md   # send from Markdown file
mailctl draft msg.md  # save draft
mailctl search QUERY  # search messages
mailctl thread ID     # show thread
mailctl mcp           # start MCP server (stdio)
```

### Email Markdown format

```markdown
---
to: [recipient@example.com]
cc: []
subject: Hello there
---

Message body here.
```

## MCP Server

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "mailctl": {
      "command": "mailctl",
      "args": ["mcp"]
    }
  }
}
```

Available tools: `list_messages`, `get_message_body`, `search_messages`, `send_message`, `get_thread`

## Architecture

```
Apple Mail (AppleScript)
    ↓ sync
SQLite (~/.local/share/mailctl/mail.db)
    ↓
TUI (Bubbletea)  ←→  MCP Server (stdio)
```
