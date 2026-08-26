# Thunderbird/Linux backend for mailctl

Status: approved design, not yet implemented.
Date: 2026-08-26

## Problem

mailctl currently only works on macOS: `internal/mail/apple.go` drives Apple
Mail via AppleScript, and every caller (`cmd/`, the MCP server, the TUI)
calls its free functions directly (`FetchInbox`, `Send`, `SearchMessages`,
`ListAccounts`, ...). There is no Linux support and no abstraction that
would let a second backend exist alongside it.

Goal: add a Linux backend that reads mail from a local Thunderbird
installation, with the same CLI/MCP/TUI surface mailctl already has on
macOS.

## Scope

In scope:
- Reading the Inbox of every IMAP account in the user's default Thunderbird
  profile (parity with the current Apple Mail behavior: all accounts, inbox
  only).
- Sending mail via the account's own SMTP server (STARTTLS, password auth).
- A `mailctl account set-password` command to store an SMTP password in the
  OS keyring.

Out of scope (v1, deliberately deferred):
- Maildir mailbox format (mbox only — Thunderbird's default, unchanged by
  the vast majority of users).
- OAuth2 / modern Gmail-style auth, and implicit TLS (port 465). STARTTLS +
  password only.
- An `OpenInMail`-equivalent — Thunderbird has no AppleScript-style remote
  control API. The Linux implementation returns a clear "not supported on
  Linux" error instead of crashing.
- calctl / Linux calendar support — separate effort, separate spec.

## Architecture

### Build-tag functions, no interface

`internal/mail/apple.go` gets `//go:build darwin`. A new
`internal/mail/thunderbird.go` gets `//go:build linux` and implements the
same function signatures: `FetchInbox`, `FetchMessageBody`,
`SearchMessages`, `FetchThread`, `ListAccounts`, `Send`, `SaveDraft`,
`MarkUnreadInMail`, `DeleteInMail`, `OpenInMail`.

No `Source` interface, no factory, no runtime dispatch: because each
platform's binary only ever compiles one of the two files, Go's build tags
already provide the polymorphism. Callers (`cmd/`, `internal/mail/sync.go`,
`internal/mcpserver`, `internal/tui`) are unchanged — they keep calling
`mail.FetchInbox(...)` etc. exactly as today.

The one shared touch point: `sync.go` currently hardcodes
`s.DeleteBySource(ctx, "apple")`. Each platform file defines
`const sourceName = "apple"` / `const sourceName = "thunderbird"`, and
`sync.go` uses `sourceName` instead of the literal.

### New files (linux-only, `//go:build linux`)

- `internal/mail/thunderbird.go` — the public functions listed above.
- `internal/mail/thunderbird_profile.go` — profile resolution
  (`profiles.ini`) and `prefs.js` parsing into account/server structs.
- `internal/mail/thunderbird_mbox.go` — mbox parser.
- `internal/mail/thunderbird_smtp.go` — send/save-draft via `net/smtp`.

### New package (no build tag — used by both platforms, but only actually
called on Linux for now)

- `internal/keyring/keyring.go` — thin wrapper around a keyring library
  (e.g. `zalando/go-keyring`: Secret Service/libsecret on Linux, Keychain
  on macOS) for storing/retrieving the SMTP password. This is the one new
  Go dependency this spec introduces.

## Data flow

1. **Profile resolution.** Parse `~/.thunderbird/profiles.ini` (plain INI,
   `bufio.Scanner` — no dependency needed). Pick the profile marked
   `Default=1`; if multiple `[Install...]` sections declare a default,
   use the most recently modified one. No profile found → a clear sync
   error, not a silent empty result (matches today's behavior when Apple
   Mail isn't running).

2. **Account/server configuration.** Parse `<profile>/prefs.js` line by
   line, collecting `user_pref("mail.server.<id>.*", ...)` and
   `user_pref("mail.smtpserver.<id>.*", ...)` entries into per-account
   structs: `{hostname, port, username, directory, smtpHost, smtpPort}`.
   Non-IMAP servers (News/RSS/Local Folders) are skipped.

3. **Reading the inbox.** For each account, parse
   `<directory>/INBOX` (the mbox file itself, not the `.msf` index) —
   messages are split on `From ` envelope lines at the start of a line,
   then parsed as standard RFC822 header + body. Mapped to
   `models.Message` the same way as the Apple backend: `Account` =
   username, `Mailbox` = "INBOX", `Source` = "thunderbird".

4. **Sending.** `net/smtp` with STARTTLS against `smtpHost:smtpPort` from
   prefs.js, PLAIN/LOGIN auth, password looked up from the keyring by
   account email.

   > ponytail: STARTTLS + password only, no implicit TLS (465) or OAuth2.
   > Upgrade path: add a TLS-first dial for port 465, and an OAuth2 device
   > flow for providers that require it, if/when a real account needs it.

## Credentials

`mailctl account set-password <email>` prompts interactively for the app
password (no plaintext flag, no terminal echo) and stores it in the OS
keyring under service `mailctl`, key `<email>`. `mailctl send` / `mailctl
draft` fail with a clear error if no password is stored for the sending
account: "no password stored for `<email>` — run `mailctl account
set-password <email>`".

## Error handling

- No Thunderbird profile found → one clear error on `sync`, not a silent
  empty result.
- An account's mbox file is missing or empty (no mail yet) → skipped, not
  an error.
- A malformed/partial mbox entry (e.g. Thunderbird is mid-write) → skip
  that one message, keep processing the rest of the file; don't abort the
  whole sync.
- SMTP errors (bad password, network) → surfaced directly, same as the
  existing `mail.Send` behavior today.

## Testing

The mbox parser and the prefs.js parser are pure, deterministic
string→struct functions — each gets a `_test.go` with 2-3 fixtures:
- mbox fixture with 2 messages, including an edge case (a `From ` line
  embedded in a message body, which must not be treated as a new envelope).
- prefs.js fixture with 2 accounts (one IMAP, one non-IMAP server that
  must be skipped).

No mocking of Thunderbird itself is needed — that's the payoff of the
file-based approach.

## Open questions

None — all prior open questions were resolved during design (send-path
credential model, build model, account scope, mbox-only for v1, keyring
for password storage, build-tag functions over an interface).
