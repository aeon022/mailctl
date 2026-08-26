# mailctl

Terminal email client for Apple Mail (macOS) or Thunderbird (Linux). Part of the [missionctl](https://github.com/aeon022/missionctl) suite.

Syncs your Apple Mail inbox into a local SQLite cache, provides a fast full-screen TUI, and exposes an MCP server so AI agents (Claude Desktop, etc.) can read and send email on your behalf.

---

## Quick Start

1. **Clone and build**

   ```bash
   git clone https://github.com/aeon022/mailctl && cd mailctl
   ./setup.sh
   ```

2. **Verify Apple Mail is running** with at least one configured account.

3. **Sync your inbox**

   ```bash
   mailctl sync
   ```

4. **Open the TUI**

   ```bash
   mailctl
   ```

5. **Optional — connect to Claude Desktop** (see [MCP — AI Integration](#mcp--ai-integration)).

---

## Linux (Thunderbird)

On Linux, mailctl reads mail directly from your default Thunderbird
profile's local mbox files, and sends via that account's own SMTP server.

1. Install and run Thunderbird at least once, with at least one IMAP
   account configured.
2. Store the account's SMTP app password (used for sending; not needed
   just to read/sync):
   ```bash
   mailctl account set-password you@example.com
   ```
3. `mailctl sync`, `mailctl`, etc. work the same as on macOS.

Current limitations: mbox mailbox format only (not Maildir), inbox only
(no other folders), STARTTLS + password auth only (no implicit TLS on
port 465, no OAuth2), no attachments on send, and no draft-saving or
delete/mark-unread from mailctl — those would require writing into a
Thunderbird mbox file it may have open, which mailctl avoids.

---

## Cheatsheet

```
mailctl [tui]                       Open TUI (default)
mailctl sync [--live]               Sync inbox from Apple Mail
mailctl inbox [--count N] [--unread] [--live] [--json]
mailctl search QUERY [--json]       Search subject / from / body
mailctl send draft.md               Send from Markdown file
mailctl draft draft.md              Save to Drafts
mailctl thread SUBJECT [--json]     Show thread by subject
mailctl accounts [--json]           List Apple Mail accounts
mailctl mcp                         Start MCP server (stdio)
```

TUI list view — `j/k` navigate · `enter` open · `n` new · `s` sync · `/` search · `d` delete · `q` quit

TUI detail view — `esc` back · `r` reply · `o` open in Mail · `U` unsubscribe · `q` quit

---

## CLI Reference

### `mailctl` / `mailctl tui`

Open the full-screen TUI. No flags.

```bash
mailctl
mailctl tui
```

---

### `mailctl sync`

Pull messages from Apple Mail into the local SQLite cache via AppleScript.

| Flag | Description |
|------|-------------|
| `--live` | Stream sync progress to stdout |

```bash
mailctl sync
mailctl sync --live
```

---

### `mailctl inbox`

List messages from the cached inbox.

| Flag | Description |
|------|-------------|
| `--count N` | Return at most N messages (default: 50) |
| `--unread` | Show only unread messages |
| `--live` | Re-sync from Apple Mail before listing |
| `--json` | Output as JSON |

```bash
mailctl inbox
mailctl inbox --count 20 --unread
mailctl inbox --live --json
```

---

### `mailctl search`

Search the local cache across subject, sender, and body.

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |

```bash
mailctl search "project proposal"
mailctl search "invoice" --json
```

---

### `mailctl send`

Send an email from a Markdown file. See [Email Template Format](#email-template-format) for the file structure.

```bash
mailctl send draft.md
```

---

### `mailctl draft`

Save a Markdown email file to Apple Mail Drafts without sending.

```bash
mailctl draft draft.md
```

---

### `mailctl thread`

Retrieve all messages in a thread matched by subject string.

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |

```bash
mailctl thread "Q3 Budget Review"
mailctl thread "Weekly standup" --json
```

---

### `mailctl accounts`

List all accounts configured in Apple Mail.

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |

```bash
mailctl accounts
mailctl accounts --json
```

---

### `mailctl mcp`

Start the MCP server on stdio. Used by Claude Desktop and other MCP-compatible clients — not intended for interactive use.

```bash
mailctl mcp
```

---

### `mailctl license`

Activate or check your [missionctl Bundle](https://missionctl.sh/#pricing) license, which unlocks the `a` (AI draft reply) key.

```bash
mailctl license activate <key>
mailctl license status
```

---

## TUI Keys

### List view

| Key | Action |
|-----|--------|
| `j` / `k` or `↓` / `↑` | Move down / up |
| `PgDn` / `PgUp` | Page down / up |
| `g` / `G` | Jump to first / last message |
| `enter` | Open message detail |
| `n` | Compose new message |
| `s` | Sync inbox from Apple Mail |
| `u` | Toggle unread-only filter |
| `/` | Search (subject, from, body) |
| `tab` / `shift+tab` | Switch account |
| `d` | Delete selected message (press `d` again to confirm, `esc` to cancel) |
| `y` | Copy subject + sender to clipboard |
| `q` | Quit |

### Detail view

| Key | Action |
|-----|--------|
| `esc` | Back to list |
| `j` / `k` or `↑` / `↓` | Scroll body |
| `r` | Reply (opens compose with quoted text) |
| `a` | AI draft reply (missionctl Bundle feature, see Requirements) |
| `u` | Mark as unread |
| `U` | Open unsubscribe link in browser (shown only when detected) |
| `d` | Delete message |
| `o` | Open in Apple Mail |
| `y` | Copy message to clipboard |
| `q` | Quit |

### Compose view

| Key | Action |
|-----|--------|
| `tab` / `shift+tab` | Cycle fields (To, Subject, Attachments, Body) |
| `ctrl+s` | Send |
| `ctrl+d` | Save to Drafts |
| `esc` | Cancel |

---

## Email Template Format

Both `mailctl send` and `mailctl draft` accept a Markdown file with a YAML front matter header.

```markdown
---
to: [alice@example.com, bob@example.com]
cc: [manager@example.com]
subject: Q3 Report — Draft for Review
account: work@company.com
attachments: [/path/to/report.pdf, /path/to/appendix.xlsx]
vars:
  name: Alice
  deadline: Friday
---

Hi {{index . "name"}},

Please find the Q3 report attached. The review deadline is {{index . "deadline"}}.

Sent on {{index . "date"}} — internal reference {{index . "year"}}.
```

**Front matter fields**

| Field | Required | Description |
|-------|----------|-------------|
| `to` | Yes | List of recipient addresses |
| `cc` | No | List of CC addresses |
| `subject` | Yes | Email subject line |
| `account` | No | Sender account; uses Apple Mail default if omitted |
| `attachments` | No | List of absolute file paths to attach |
| `vars` | No | Key/value pairs available as template variables |

**Template variables**

Variables in the body are Go template expressions using `{{index . "key"}}`.

| Variable | Source | Example output |
|----------|--------|----------------|
| `{{index . "name"}}` | `vars.name` in front matter | `Alice` |
| `{{index . "date"}}` | Built-in | `July 4, 2026` |
| `{{index . "year"}}` | Built-in | `2026` |
| Any other key | `vars.*` in front matter | Custom value |

---

## MCP — AI Integration

mailctl includes a built-in MCP (Model Context Protocol) server that exposes your inbox and sending capabilities to AI agents such as Claude Desktop.

### Configuration

Add the following to `~/Library/Application Support/Claude/claude_desktop_config.json`:

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

Restart Claude Desktop. The `mailctl` tools will appear automatically.

### Available MCP tools

| Tool | Description |
|------|-------------|
| `inbox` | List recent inbox messages with sender, subject, and body preview |
| `search_email` | Search across subject, sender, and body by keyword |
| `email_thread` | Retrieve all messages in a thread matched by subject |
| `send_email` | Send an email (body, to, subject; optional cc, bcc, account) |
| `draft_email` | Save a composed message to Apple Mail Drafts |
| `sync_inbox` | Trigger a sync from Apple Mail into the local cache |

### Practical workflows

**Morning email briefing**

Ask Claude: *"Summarize my unread emails from the past 24 hours, grouped by sender. Flag anything that looks urgent."*

Claude calls `sync_inbox` to get current data, then `inbox` with an unread filter, and returns a structured summary without you opening your mail client.

**Send a templated message to multiple contacts**

Prepare a recipient list and ask Claude: *"Send the attached onboarding template to each person on this list, personalizing the greeting with their first name."*

Claude calls `send_email` once per recipient, substituting the name field each time. The account field can be specified per call to send from the correct address.

**Find and respond to a thread**

Ask Claude: *"Find the thread about the vendor contract and draft a reply saying we need one more week."*

Claude calls `search_email` to locate the thread, `email_thread` to retrieve the full context, then `draft_email` to save a reply for your review before sending.

---

## Architecture

```
Apple Mail (AppleScript)  —sync—>  SQLite (~/.local/share/mailctl/mail.db)
                                          |
                          +---------------+---------------+
                          |                               |
                   TUI (Bubbletea)              MCP server (stdio)
                   mailctl / mailctl tui        mailctl mcp
```

AppleScript bridges mailctl to Apple Mail for all read and write operations (sync, send, draft, delete, open). The SQLite store uses WAL mode by default for fast concurrent reads (see below for syncing across devices). The TUI and MCP server both query the same database, so a sync triggered from the TUI is immediately visible to an AI agent and vice versa.

---

## Syncing across devices

By default mailctl's cache lives at `~/Library/Application Support/mailctl/mailctl.db`, local to this machine. To share it across devices, set `data_dir` (in `~/.config/mailctl/config.yaml`) or the `MAILCTL_DATA_DIR` env var to a folder you already sync yourself — iCloud Drive, Dropbox, Syncthing, etc:

```bash
export MAILCTL_DATA_DIR="$HOME/Library/Mobile Documents/com~apple~CloudDocs/mailctl"
```

Once set, mailctl automatically switches its SQLite journal mode from WAL to rollback-journal — WAL splits the database across multiple files that a folder-sync client can't update atomically together, so this switch keeps the directory down to a single consistent file whenever mailctl isn't actively writing. A same-machine lock also prevents two mailctl processes from opening the cache at once (run `mailctl doctor` to see the current mode and path). This only protects against the same-machine and stale-snapshot failure modes, not two machines editing at the exact same instant; an undownloaded iCloud file is reported explicitly rather than as a bare error.

---

## Requirements

- macOS with Apple Mail configured and at least one account
- Go 1.21+
- The `a` (AI draft reply) key requires an active [missionctl Bundle](https://missionctl.sh/#pricing) license (`mailctl license activate <key>`), plus one of: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, a free `GEMINI_API_KEY` ([aistudio.google.com/apikey](https://aistudio.google.com/apikey), no card required), or a locally running [Ollama](https://ollama.com) — auto-detected in that order, override with `MAILCTL_PROVIDER`. Everything else works fully without a license or any of these.

## License

See [LICENSE](LICENSE).
