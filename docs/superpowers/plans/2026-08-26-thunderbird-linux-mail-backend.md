# Thunderbird/Linux Mail Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give mailctl a Linux backend that reads mail from a local Thunderbird installation and sends via the account's own SMTP server, with the same CLI/MCP/TUI surface it already has on macOS via Apple Mail.

**Architecture:** `internal/mail/apple.go` becomes darwin-only (`//go:build darwin`); a new `internal/mail/thunderbird.go` becomes linux-only (`//go:build linux`) and implements the same exported function names (`FetchInbox`, `Send`, `ListAccounts`, ...), so every existing caller (`cmd/`, `internal/mcpserver`, `internal/tui`) is untouched — Go's build tags provide the platform dispatch, no interface or factory needed. The parsing logic underneath (Thunderbird profile/prefs.js resolution, mbox parsing, MIME building) lives in **untagged** sibling files so it compiles and unit-tests on any OS, including this darwin dev machine — only the thin `thunderbird.go` glue file that would otherwise collide with `apple.go`'s function names needs the linux tag.

**Tech Stack:** Go 1.26, stdlib (`net/smtp`, `net/mail`, `bufio`), `github.com/zalando/go-keyring` v0.2.8 (new — OS keyring for the SMTP password), `golang.org/x/term` v0.45.0 (new — hidden password prompt).

**Spec:** `docs/superpowers/specs/2026-08-26-thunderbird-linux-backend-design.md`

## Global Constraints

- mbox format only — no Maildir (spec's explicit v1 scope cut).
- STARTTLS + password auth only — no implicit TLS (port 465), no OAuth2 (spec's explicit v1 scope cut).
- Inbox only, all accounts in the default Thunderbird profile (spec's explicit scope; matches existing Apple Mail behavior).
- SMTP password lives in the OS keyring (service `mailctl`, key = account email) — never written to `~/.config/mailctl/config.yaml`.
- No write-back into a live Thunderbird mbox file: `MarkUnreadInMail`, `DeleteInMail`, `OpenInMail`, and `SaveDraft` all return a clear "not supported on Linux" error instead of touching a file Thunderbird may have open. (`SaveDraft` was not in the original spec's mutation list, but writing a Drafts-folder mbox entry carries the identical corruption risk the spec ruled out for delete/mark-unread — same reasoning applies, flagged here explicitly since it wasn't spelled out in the spec.)
- Attachments are not supported on the Linux send path for v1 (multipart MIME wasn't part of the discussed design) — `Send` returns a clear error rather than silently dropping them.
- Every pure-logic file (profile/prefs parsing, mbox parsing, MIME building) carries **no** build tag, so `go test ./...` on this darwin machine actually exercises it. Only `internal/mail/thunderbird.go` (whose function names collide with `apple.go`) needs `//go:build linux`. That file can only be verified with `GOOS=linux go build ./...` (cross-compile) on this machine — real functional verification needs a Linux host with Thunderbird installed, which this plan cannot do; say so rather than claim it's tested.

---

## Task 1: Make the platform split buildable — rename `SyncFromApple`, tag `apple.go`

**Files:**
- Modify: `internal/mail/apple.go`
- Modify: `internal/mail/sync.go`
- Modify: `cmd/sync.go`
- Modify: `internal/tui/tui.go:1567`
- Modify: `internal/mcpserver/server.go:168`

**Interfaces:**
- Consumes: nothing new — this is a pure rename/retag of existing code.
- Produces: `mail.Sync(count int) (msgs []models.Message, accounts []string, err error)` (was `SyncFromApple`), and the package-private `const sourceName` pattern that Task 7's `thunderbird.go` will also define.

This has to happen first: `apple.go` currently defines `FetchInbox`, `Send`, etc. with no build tag. Before `thunderbird.go` (Task 7) can define the same names under `//go:build linux`, `apple.go` must be restricted to `//go:build darwin` or the two would collide the moment both existed. `cmd/sync.go`, `tui.go`, and `mcpserver/server.go` all call `mail.SyncFromApple` by that literal name — the spec assumed callers were untouched, but this one function name is Apple-specific and has to become platform-neutral too.

- [ ] **Step 1: Add the build tag and a `sourceName` const to apple.go**

At the very top of `internal/mail/apple.go`, before `package mail`:

```go
//go:build darwin

package mail
```

Then add, near the top of the file (after the imports):

```go
const sourceName = "apple"
```

And in `parseMessages`, change:

```go
		m := models.Message{
			ID:      uuid.New().String(),
			Mailbox: defaultMailbox,
			Source:  "apple",
		}
```

to:

```go
		m := models.Message{
			ID:      uuid.New().String(),
			Mailbox: defaultMailbox,
			Source:  sourceName,
		}
```

- [ ] **Step 2: Rename `SyncFromApple` to `Sync` and use `sourceName` in sync.go**

In `internal/mail/sync.go`, change the doc comment and signature:

```go
// Sync fetches inbox messages from the platform mail source (Apple Mail on
// macOS, Thunderbird on Linux) and replaces this source's rows in the local
// store with them, returning the synced messages and the distinct account
// names now in the store. Shared by the CLI sync command, MCP sync tool,
// and TUI — all three previously duplicated this fetch+store+DeleteBySource+
// UpsertMessage loop verbatim.
func Sync(count int) (msgs []models.Message, accounts []string, err error) {
	msgs, err = FetchInbox(count, false)
	if err != nil {
		return nil, nil, err
	}
	s, err := store.New(config.DBPath(), config.Shared())
	if err != nil {
		return nil, nil, err
	}
	defer s.Close()
	ctx := context.Background()
	_ = s.DeleteBySource(ctx, sourceName)
	for i := range msgs {
		_ = s.UpsertMessage(ctx, &msgs[i])
	}
	accounts, _ = s.ListAccounts(ctx)
	return msgs, accounts, nil
}
```

- [ ] **Step 3: Update the three call sites**

In `cmd/sync.go`, change:
```go
		msgs, _, err := mail.SyncFromApple(syncCount)
```
to:
```go
		msgs, _, err := mail.Sync(syncCount)
```

In `internal/tui/tui.go` (line 1567), change:
```go
		msgs, accounts, err := mail.SyncFromApple(150)
```
to:
```go
		msgs, accounts, err := mail.Sync(150)
```

In `internal/mcpserver/server.go` (line 168), change:
```go
	msgs, _, err := mail.SyncFromApple(count)
```
to:
```go
	msgs, _, err := mail.Sync(count)
```

- [ ] **Step 4: Build and run the full test suite to confirm nothing broke**

Run: `go build ./... && go test ./...`
Expected: builds clean, all existing tests pass (this task is a pure rename/retag — no behavior change on darwin).

- [ ] **Step 5: Commit**

```bash
git add internal/mail/apple.go internal/mail/sync.go cmd/sync.go internal/tui/tui.go internal/mcpserver/server.go
git commit -m "mail: tag apple.go darwin-only, rename SyncFromApple to Sync"
```

---

## Task 2: Platform-aware application-support directory

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go` (new)

**Interfaces:**
- Consumes: nothing new.
- Produces: `appSupportDir() string` — used internally by `DBPath()` and `appFile()`. No exported surface changes.

`config.DBPath()` and `config.appFile()` currently hardcode `~/Library/Application Support/mailctl`, which doesn't exist on Linux. Split the directory choice into a pure, parameterized function so it's testable without needing to actually run on Linux.

- [ ] **Step 1: Write the failing test**

Create `internal/config/config_test.go`:

```go
package config

import (
	"path/filepath"
	"testing"
)

func TestAppSupportDirFor(t *testing.T) {
	cases := []struct {
		goos, home, xdgDataHome, want string
	}{
		{"darwin", "/Users/jan", "", "/Users/jan/Library/Application Support/mailctl"},
		{"linux", "/home/jan", "", "/home/jan/.local/share/mailctl"},
		{"linux", "/home/jan", "/home/jan/.custom-data", "/home/jan/.custom-data/mailctl"},
	}
	for _, c := range cases {
		t.Setenv("XDG_DATA_HOME", c.xdgDataHome)
		got := appSupportDirFor(c.goos, c.home)
		want := filepath.FromSlash(c.want)
		if got != want {
			t.Errorf("appSupportDirFor(%q, %q) with XDG_DATA_HOME=%q = %q, want %q",
				c.goos, c.home, c.xdgDataHome, got, want)
		}
	}
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./internal/config/... -run TestAppSupportDirFor -v`
Expected: FAIL — `appSupportDirFor` is undefined.

- [ ] **Step 3: Implement `appSupportDirFor` and `appSupportDir`, use them in `DBPath`/`appFile`**

In `internal/config/config.go`, add `"runtime"` to the imports, then add:

```go
// appSupportDirFor returns the OS-appropriate application-support directory
// for the given goos/home — a pure function (goos as a parameter, not
// runtime.GOOS directly) so both branches are unit-testable from a single
// compiled test binary, regardless of which OS actually runs the test.
func appSupportDirFor(goos, home string) string {
	if goos == "linux" {
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "mailctl")
		}
		return filepath.Join(home, ".local", "share", "mailctl")
	}
	return filepath.Join(home, "Library", "Application Support", "mailctl")
}

func appSupportDir() string {
	home, _ := os.UserHomeDir()
	return appSupportDirFor(runtime.GOOS, home)
}
```

Then in `DBPath()`, replace:
```go
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Application Support", "mailctl")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "mailctl.db")
```
with:
```go
	dir := appSupportDir()
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "mailctl.db")
```

And in `appFile(name string)`, replace:
```go
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Application Support", "mailctl")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, name)
```
with:
```go
	dir := appSupportDir()
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, name)
```

- [ ] **Step 4: Run the test again to confirm it passes**

Run: `go test ./internal/config/... -run TestAppSupportDirFor -v`
Expected: PASS

- [ ] **Step 5: Run the full test suite**

Run: `go build ./... && go test ./...`
Expected: builds clean, all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "config: resolve app-support dir per OS instead of hardcoding macOS path"
```

---

## Task 3: Thunderbird profile and prefs.js parsing

**Files:**
- Create: `internal/mail/thunderbird_profile.go`
- Test: `internal/mail/thunderbird_profile_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `type tbAccount struct { Email, Hostname string; Port int; Username, Directory, SMTPHost string; SMTPPort int }`
  - `func thunderbirdAccounts() ([]tbAccount, error)` — Task 7 (`thunderbird.go`) calls this.
  - `func ThunderbirdProfileFound() (string, bool)` — exported; Task 9 (`cmd/doctor.go`) calls this.

No build tag: this is pure file/string parsing, safe to compile and test on any OS. It only touches the filesystem when actually called (which only happens from linux-tagged `thunderbird.go`, or from `doctor.go`'s Linux branch).

- [ ] **Step 1: Write the failing tests**

Create `internal/mail/thunderbird_profile_test.go`:

```go
package mail

import "testing"

const testProfilesINI = `[Profile0]
Name=default
IsRelative=1
Path=abc123.default-release
Default=1

[Install4F96D1932A9F858F]
Default=abc123.default-release
Locked=1
`

const testProfilesININoInstall = `[Profile0]
Name=default
IsRelative=1
Path=xyz789.default-release
Default=1
`

func TestParseINI(t *testing.T) {
	sections := parseINI([]byte(testProfilesINI))
	if len(sections) != 2 {
		t.Fatalf("got %d sections, want 2", len(sections))
	}
	if sections[0].name != "Profile0" || sections[0].props["Path"] != "abc123.default-release" {
		t.Errorf("unexpected Profile0 section: %+v", sections[0])
	}
	if sections[1].name != "Install4F96D1932A9F858F" || sections[1].props["Default"] != "abc123.default-release" {
		t.Errorf("unexpected Install section: %+v", sections[1])
	}
}

func TestResolveDefaultProfileDir_PrefersInstallSection(t *testing.T) {
	sections := parseINI([]byte(testProfilesINI))
	dir, err := resolveDefaultProfileDir(sections, "/home/jan/.thunderbird")
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/home/jan/.thunderbird/abc123.default-release" {
		t.Errorf("got %q", dir)
	}
}

func TestResolveDefaultProfileDir_FallsBackToProfileDefault(t *testing.T) {
	sections := parseINI([]byte(testProfilesININoInstall))
	dir, err := resolveDefaultProfileDir(sections, "/home/jan/.thunderbird")
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/home/jan/.thunderbird/xyz789.default-release" {
		t.Errorf("got %q", dir)
	}
}

func TestResolveDefaultProfileDir_NoDefault(t *testing.T) {
	sections := parseINI([]byte("[Profile0]\nName=x\n"))
	if _, err := resolveDefaultProfileDir(sections, "/home/jan/.thunderbird"); err == nil {
		t.Fatal("want error when no default profile is declared")
	}
}

const testPrefsJS = `user_pref("mail.accountmanager.accounts", "account1,account2");
user_pref("mail.account.account1.identities", "id1");
user_pref("mail.account.account1.server", "server1");
user_pref("mail.account.account2.server", "server2");
user_pref("mail.identity.id1.useremail", "jan@example.com");
user_pref("mail.identity.id1.smtpServer", "smtp1");
user_pref("mail.server.server1.type", "imap");
user_pref("mail.server.server1.hostname", "imap.example.com");
user_pref("mail.server.server1.port", 993);
user_pref("mail.server.server1.userName", "jan@example.com");
user_pref("mail.server.server1.directory", "/home/jan/.thunderbird/abc123.default-release/ImapMail/imap.example.com");
user_pref("mail.smtpserver.smtp1.hostname", "smtp.example.com");
user_pref("mail.smtpserver.smtp1.port", 587);
user_pref("mail.server.server2.type", "rss");
user_pref("mail.server.server2.hostname", "");
`

func TestParsePrefsJS(t *testing.T) {
	prefs := parsePrefsJS([]byte(testPrefsJS))
	if prefs["mail.server.server1.hostname"] != "imap.example.com" {
		t.Errorf("got %q", prefs["mail.server.server1.hostname"])
	}
	if prefs["mail.server.server1.port"] != "993" {
		t.Errorf("got %q", prefs["mail.server.server1.port"])
	}
}

func TestBuildTBAccounts_SkipsNonIMAPAndFillsSMTP(t *testing.T) {
	prefs := parsePrefsJS([]byte(testPrefsJS))
	accounts := buildTBAccounts(prefs)
	if len(accounts) != 1 {
		t.Fatalf("got %d accounts, want 1 (rss account2 should be skipped)", len(accounts))
	}
	a := accounts[0]
	if a.Email != "jan@example.com" {
		t.Errorf("Email = %q", a.Email)
	}
	if a.Hostname != "imap.example.com" || a.Port != 993 {
		t.Errorf("Hostname/Port = %q/%d", a.Hostname, a.Port)
	}
	if a.SMTPHost != "smtp.example.com" || a.SMTPPort != 587 {
		t.Errorf("SMTPHost/SMTPPort = %q/%d", a.SMTPHost, a.SMTPPort)
	}
	if a.Directory != "/home/jan/.thunderbird/abc123.default-release/ImapMail/imap.example.com" {
		t.Errorf("Directory = %q", a.Directory)
	}
}
```

- [ ] **Step 2: Run to confirm the tests fail**

Run: `go test ./internal/mail/... -run 'TestParseINI|TestResolveDefaultProfileDir|TestParsePrefsJS|TestBuildTBAccounts' -v`
Expected: FAIL — none of these functions/types exist yet.

- [ ] **Step 3: Implement thunderbird_profile.go**

Create `internal/mail/thunderbird_profile.go`:

```go
package mail

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// tbAccount is one IMAP account resolved from a Thunderbird profile's
// prefs.js — enough to locate its inbox mbox file and, separately, its
// SMTP server for sending.
type tbAccount struct {
	Email     string
	Hostname  string
	Port      int
	Username  string
	Directory string
	SMTPHost  string
	SMTPPort  int
}

type iniSection struct {
	name  string
	props map[string]string
}

// parseINI parses the subset of INI syntax profiles.ini actually uses:
// `[Section]` headers and `key=value` lines, nothing else (no comments
// inside values, no multi-line values).
func parseINI(data []byte) []iniSection {
	var sections []iniSection
	var current *iniSection
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			sections = append(sections, iniSection{name: line[1 : len(line)-1], props: map[string]string{}})
			current = &sections[len(sections)-1]
			continue
		}
		if current == nil {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		current.props[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return sections
}

// resolveDefaultProfileDir picks the profile Thunderbird itself would use.
// A `[InstallXXXX]` section's `Default=` (an install-specific relative
// path) takes precedence when present — that's what modern Thunderbird
// actually consults first. profiles.ini carries no timestamps, so if more
// than one Install section somehow declares a default, this takes the
// first one rather than guessing at "most recent" from data that isn't
// there. Falls back to the classic `[ProfileN] Default=1` marker.
func resolveDefaultProfileDir(sections []iniSection, thunderbirdDir string) (string, error) {
	for _, s := range sections {
		if strings.HasPrefix(s.name, "Install") {
			if p := s.props["Default"]; p != "" {
				return filepath.Join(thunderbirdDir, p), nil
			}
		}
	}
	for _, s := range sections {
		if strings.HasPrefix(s.name, "Profile") && s.props["Default"] == "1" {
			path := s.props["Path"]
			if path == "" {
				continue
			}
			if s.props["IsRelative"] == "0" {
				return path, nil
			}
			return filepath.Join(thunderbirdDir, path), nil
		}
	}
	return "", fmt.Errorf("no default profile found in profiles.ini")
}

// parsePrefsJS reads every `user_pref("key", value);` line into a flat
// map. Thunderbird's prefs.js has no nesting — every setting is one such
// line — so a flat string map is all the structure this needs.
func parsePrefsJS(data []byte) map[string]string {
	prefs := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		const prefix = `user_pref("`
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rest := line[len(prefix):]
		keyEnd := strings.Index(rest, `"`)
		if keyEnd < 0 {
			continue
		}
		key := rest[:keyEnd]
		rest = strings.TrimSpace(rest[keyEnd+1:])
		rest = strings.TrimPrefix(rest, ",")
		rest = strings.TrimSpace(rest)
		rest = strings.TrimSuffix(rest, ";")
		val := strings.TrimSpace(rest)
		if len(val) >= 2 && strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`) {
			val = val[1 : len(val)-1]
			val = strings.ReplaceAll(val, `\"`, `"`)
			val = strings.ReplaceAll(val, `\\`, `\`)
		}
		prefs[key] = val
	}
	return prefs
}

// buildTBAccounts turns the flat prefs map into one tbAccount per IMAP
// account, skipping POP3/RSS/NNTP/Local-Folders entries entirely — those
// have no inbox to read via mbox in the sense mailctl cares about here.
func buildTBAccounts(prefs map[string]string) []tbAccount {
	var accounts []tbAccount
	for _, accID := range strings.Split(prefs["mail.accountmanager.accounts"], ",") {
		accID = strings.TrimSpace(accID)
		if accID == "" {
			continue
		}
		serverID := prefs["mail.account."+accID+".server"]
		if serverID == "" {
			continue
		}
		if prefs["mail.server."+serverID+".type"] != "imap" {
			continue
		}
		port, _ := strconv.Atoi(prefs["mail.server."+serverID+".port"])
		a := tbAccount{
			Hostname:  prefs["mail.server."+serverID+".hostname"],
			Port:      port,
			Username:  prefs["mail.server."+serverID+".userName"],
			Directory: prefs["mail.server."+serverID+".directory"],
		}
		identIDs := strings.Split(prefs["mail.account."+accID+".identities"], ",")
		if len(identIDs) > 0 && strings.TrimSpace(identIDs[0]) != "" {
			identID := strings.TrimSpace(identIDs[0])
			a.Email = prefs["mail.identity."+identID+".useremail"]
			if smtpID := prefs["mail.identity."+identID+".smtpServer"]; smtpID != "" {
				a.SMTPHost = prefs["mail.smtpserver."+smtpID+".hostname"]
				a.SMTPPort, _ = strconv.Atoi(prefs["mail.smtpserver."+smtpID+".port"])
			}
		}
		if a.Email == "" {
			a.Email = a.Username
		}
		accounts = append(accounts, a)
	}
	return accounts
}

// defaultThunderbirdProfileDir resolves ~/.thunderbird/profiles.ini to the
// default profile's directory.
func defaultThunderbirdProfileDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	tbDir := filepath.Join(home, ".thunderbird")
	data, err := os.ReadFile(filepath.Join(tbDir, "profiles.ini"))
	if err != nil {
		return "", fmt.Errorf("read profiles.ini: %w", err)
	}
	return resolveDefaultProfileDir(parseINI(data), tbDir)
}

// thunderbirdAccounts resolves the default profile and returns every IMAP
// account configured in it.
func thunderbirdAccounts() ([]tbAccount, error) {
	profileDir, err := defaultThunderbirdProfileDir()
	if err != nil {
		return nil, err
	}
	prefsData, err := os.ReadFile(filepath.Join(profileDir, "prefs.js"))
	if err != nil {
		return nil, fmt.Errorf("read prefs.js: %w", err)
	}
	accounts := buildTBAccounts(parsePrefsJS(prefsData))
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no IMAP accounts found in Thunderbird profile %s", profileDir)
	}
	return accounts, nil
}

// ThunderbirdProfileFound reports whether a default Thunderbird profile
// could be resolved, and its directory if so. Used by `mailctl doctor` on
// Linux, where there's no Mail.app-equivalent single app to probe.
func ThunderbirdProfileFound() (string, bool) {
	dir, err := defaultThunderbirdProfileDir()
	return dir, err == nil
}
```

- [ ] **Step 4: Run the tests again to confirm they pass**

Run: `go test ./internal/mail/... -run 'TestParseINI|TestResolveDefaultProfileDir|TestParsePrefsJS|TestBuildTBAccounts' -v`
Expected: PASS

- [ ] **Step 5: Run the full test suite**

Run: `go build ./... && go test ./...`
Expected: builds clean, all tests pass (this file has no build tag, so it's part of the normal darwin build/test too — it just isn't called from anywhere yet).

- [ ] **Step 6: Commit**

```bash
git add internal/mail/thunderbird_profile.go internal/mail/thunderbird_profile_test.go
git commit -m "mail: parse Thunderbird profiles.ini and prefs.js into IMAP accounts"
```

---

## Task 4: mbox parsing

**Files:**
- Create: `internal/mail/thunderbird_mbox.go`
- Test: `internal/mail/thunderbird_mbox_test.go`

**Interfaces:**
- Consumes: `models.Message` (existing, from `internal/models`).
- Produces:
  - `func splitMbox(data []byte) [][]byte`
  - `func parseMboxMessage(raw []byte, account, mailboxName, source string) (models.Message, error)`
  - `func parseMboxFile(path, account, mailboxName, source string) ([]models.Message, error)` — Task 7 calls this.

No build tag — pure parsing over `[]byte`, uses `net/mail.ReadMessage` for RFC822 header parsing instead of hand-rolling it. `source` is passed as a parameter (not a package-level `sourceName` const) specifically so this file doesn't need to know which OS it's running under, and so it never collides with the `sourceName` const that `apple.go` (darwin) and `thunderbird.go` (linux) each define privately in their own build-tagged file.

- [ ] **Step 1: Write the failing tests**

Create `internal/mail/thunderbird_mbox_test.go`:

```go
package mail

import "testing"

const testMbox = `From MAILER-DAEMON Mon Jan  5 09:00:00 2026
From: Jan <jan@example.com>
To: me@example.com
Subject: First message
Date: Mon, 5 Jan 2026 09:00:00 +0000
Message-Id: <msg1@example.com>
X-Mozilla-Status: 0001

Hi, this is the first message.
It mentions a line that starts with >From in the body, already escaped.

From MAILER-DAEMON Tue Jan  6 10:00:00 2026
From: Lisa <lisa@example.com>
To: me@example.com
Subject: Second message
Date: Tue, 6 Jan 2026 10:00:00 +0000
Message-Id: <msg2@example.com>
X-Mozilla-Status: 0000

Hi, this is the second, unread message.
`

func TestSplitMbox(t *testing.T) {
	parts := splitMbox([]byte(testMbox))
	if len(parts) != 2 {
		t.Fatalf("got %d messages, want 2", len(parts))
	}
}

func TestParseMboxMessage(t *testing.T) {
	parts := splitMbox([]byte(testMbox))
	m1, err := parseMboxMessage(parts[0], "jan@example.com", "INBOX", "thunderbird")
	if err != nil {
		t.Fatal(err)
	}
	if m1.Subject != "First message" {
		t.Errorf("Subject = %q", m1.Subject)
	}
	if m1.From != "Jan <jan@example.com>" {
		t.Errorf("From = %q", m1.From)
	}
	if !m1.Read {
		t.Errorf("Read = false, want true (X-Mozilla-Status 0001)")
	}
	if m1.Source != "thunderbird" || m1.Account != "jan@example.com" || m1.Mailbox != "INBOX" {
		t.Errorf("Source/Account/Mailbox = %q/%q/%q", m1.Source, m1.Account, m1.Mailbox)
	}
	if m1.Date.Year() != 2026 || m1.Date.Month().String() != "January" {
		t.Errorf("Date = %v", m1.Date)
	}

	m2, err := parseMboxMessage(parts[1], "jan@example.com", "INBOX", "thunderbird")
	if err != nil {
		t.Fatal(err)
	}
	if m2.Read {
		t.Errorf("Read = true, want false (X-Mozilla-Status 0000)")
	}
	if m2.Subject != "Second message" {
		t.Errorf("Subject = %q", m2.Subject)
	}
}
```

- [ ] **Step 2: Run to confirm the tests fail**

Run: `go test ./internal/mail/... -run 'TestSplitMbox|TestParseMboxMessage' -v`
Expected: FAIL — `splitMbox`/`parseMboxMessage` undefined.

- [ ] **Step 3: Implement thunderbird_mbox.go**

Create `internal/mail/thunderbird_mbox.go`:

```go
package mail

import (
	"bytes"
	"io"
	netmail "net/mail"
	"os"
	"strconv"
	"strings"

	"github.com/aeon022/mailctl/internal/models"
	"github.com/google/uuid"
)

// splitMbox splits raw mbox content into per-message chunks. mbox messages
// are separated by a "From " envelope line at the very start of a line;
// mbox writers (Thunderbird included) escape any body line that would
// otherwise start with "From " as ">From ", so a bare-column-0 "From " is
// unambiguous as an envelope delimiter. The envelope line itself is
// dropped — what's returned is just the RFC822 header+body that follows.
func splitMbox(data []byte) [][]byte {
	var messages [][]byte
	var current []byte
	inMsg := false
	for _, line := range bytes.Split(data, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("From ")) {
			if inMsg {
				messages = append(messages, current)
			}
			current = nil
			inMsg = true
			continue
		}
		if inMsg {
			current = append(current, line...)
			current = append(current, '\n')
		}
	}
	if inMsg {
		messages = append(messages, current)
	}
	return messages
}

// parseMboxMessage parses one RFC822 message (as produced by splitMbox)
// into a models.Message. account/mailboxName/source are stamped onto every
// message the same way apple.go's parseMessages does for its account/
// mailbox/"apple" fields.
func parseMboxMessage(raw []byte, account, mailboxName, source string) (models.Message, error) {
	m, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return models.Message{}, err
	}
	body, err := io.ReadAll(m.Body)
	if err != nil {
		return models.Message{}, err
	}
	date, _ := m.Header.Date()
	read := false
	if status := m.Header.Get("X-Mozilla-Status"); status != "" {
		if v, err := strconv.ParseUint(status, 16, 16); err == nil {
			read = v&0x0001 != 0
		}
	}
	id := m.Header.Get("Message-Id")
	if id == "" {
		id = uuid.New().String()
	}
	return models.Message{
		ID:      id,
		Subject: m.Header.Get("Subject"),
		From:    m.Header.Get("From"),
		Date:    date,
		Read:    read,
		Body:    strings.TrimSpace(string(body)),
		Mailbox: mailboxName,
		Account: account,
		Source:  source,
	}, nil
}

// parseMboxFile reads and parses an entire mbox file. A single malformed
// entry is skipped rather than aborting the whole file — Thunderbird may
// be mid-write to the last entry when this runs.
func parseMboxFile(path, account, mailboxName, source string) ([]models.Message, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var msgs []models.Message
	for _, raw := range splitMbox(data) {
		m, err := parseMboxMessage(raw, account, mailboxName, source)
		if err != nil {
			continue
		}
		if m.Subject == "" && m.From == "" {
			continue
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}
```

- [ ] **Step 4: Run the tests again to confirm they pass**

Run: `go test ./internal/mail/... -run 'TestSplitMbox|TestParseMboxMessage' -v`
Expected: PASS

- [ ] **Step 5: Run the full test suite**

Run: `go build ./... && go test ./...`
Expected: builds clean, all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/mail/thunderbird_mbox.go internal/mail/thunderbird_mbox_test.go
git commit -m "mail: add mbox parser using net/mail for RFC822 header parsing"
```

---

## Task 5: OS-keyring password storage

**Files:**
- Create: `internal/keyring/keyring.go`
- Test: `internal/keyring/keyring_test.go`
- Modify: `go.mod`, `go.sum` (adds `github.com/zalando/go-keyring`)

**Interfaces:**
- Consumes: nothing new.
- Produces: `keyring.SetPassword(email, password string) error`, `keyring.GetPassword(email string) (string, error)` — Task 7 (`thunderbird.go`) and Task 8 (`cmd/account.go`) both call these.

New dependency: `github.com/zalando/go-keyring` v0.2.8 — Secret Service/libsecret on Linux (pure Go, via `godbus/dbus`, no cgo), Keychain on macOS. Chosen over hand-rolling NSS/keychain access, and over storing the password in `~/.config/mailctl/config.yaml` (the existing pattern for the license key) because an SMTP password is a materially different sensitivity level than a license key.

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/zalando/go-keyring@v0.2.8`
Expected: `go.mod`/`go.sum` updated, no errors.

- [ ] **Step 2: Write the failing tests**

Create `internal/keyring/keyring_test.go`:

```go
package keyring

import (
	"testing"

	zkeyring "github.com/zalando/go-keyring"
)

func TestSetGetPassword(t *testing.T) {
	zkeyring.MockInit()
	if err := SetPassword("jan@example.com", "hunter2"); err != nil {
		t.Fatal(err)
	}
	got, err := GetPassword("jan@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hunter2" {
		t.Errorf("got %q, want %q", got, "hunter2")
	}
}

func TestGetPasswordNotFound(t *testing.T) {
	zkeyring.MockInit()
	if _, err := GetPassword("nobody@example.com"); err == nil {
		t.Fatal("want error for a password that was never stored")
	}
}
```

- [ ] **Step 3: Run to confirm the tests fail**

Run: `go test ./internal/keyring/... -v`
Expected: FAIL — package `internal/keyring` doesn't exist yet (build error).

- [ ] **Step 4: Implement keyring.go**

Create `internal/keyring/keyring.go`:

```go
// Package keyring stores mailctl's per-account SMTP passwords in the OS
// keyring (Secret Service/libsecret on Linux, Keychain on macOS) rather
// than in mailctl's plaintext config.yaml.
package keyring

import zkeyring "github.com/zalando/go-keyring"

const service = "mailctl"

func SetPassword(email, password string) error {
	return zkeyring.Set(service, email, password)
}

func GetPassword(email string) (string, error) {
	return zkeyring.Get(service, email)
}
```

- [ ] **Step 5: Run the tests again to confirm they pass**

Run: `go test ./internal/keyring/... -v`
Expected: PASS

- [ ] **Step 6: Run the full test suite**

Run: `go build ./... && go test ./...`
Expected: builds clean, all tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/keyring/keyring.go internal/keyring/keyring_test.go go.mod go.sum
git commit -m "keyring: add OS-keyring wrapper for SMTP passwords"
```

---

## Task 6: SMTP send path

**Files:**
- Create: `internal/mail/thunderbird_smtp.go`
- Test: `internal/mail/thunderbird_smtp_test.go`

**Interfaces:**
- Consumes: `models.Draft` (existing).
- Produces:
  - `func buildMIMEMessage(d *models.Draft, from string) []byte`
  - `func sendViaSMTP(host string, port int, username, password, from string, to []string, msg []byte) error` — Task 7 calls both.

No build tag. `sendViaSMTP` is a thin wrapper around stdlib `net/smtp.SendMail`, which negotiates STARTTLS automatically when the server advertises it — no dependency needed for SMTP itself. `sendViaSMTP` isn't unit-tested (it needs a live SMTP server); `buildMIMEMessage` is pure and fully tested.

- [ ] **Step 1: Write the failing test**

Create `internal/mail/thunderbird_smtp_test.go`:

```go
package mail

import (
	"strings"
	"testing"

	"github.com/aeon022/mailctl/internal/models"
)

func TestBuildMIMEMessage(t *testing.T) {
	d := &models.Draft{
		To:      []string{"jan@example.com"},
		CC:      []string{"lisa@example.com"},
		Subject: "Hello",
		Body:    "Hi there",
	}
	msg := string(buildMIMEMessage(d, "me@example.com"))
	for _, want := range []string{
		"From: me@example.com",
		"To: jan@example.com",
		"Cc: lisa@example.com",
		"Subject: Hello",
		"Hi there",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q, got:\n%s", want, msg)
		}
	}
	if !strings.Contains(msg, "\r\n\r\nHi there") {
		t.Errorf("expected a blank line before the body, got:\n%s", msg)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/mail/... -run TestBuildMIMEMessage -v`
Expected: FAIL — `buildMIMEMessage` undefined.

- [ ] **Step 3: Implement thunderbird_smtp.go**

Create `internal/mail/thunderbird_smtp.go`:

```go
package mail

import (
	"fmt"
	"net/smtp"
	"strings"
	"time"

	"github.com/aeon022/mailctl/internal/models"
	"github.com/google/uuid"
)

// buildMIMEMessage renders a Draft as a minimal RFC822 message: plain
// text/utf-8, no attachments, no MIME encoded-words for non-ASCII
// subjects — most clients render raw UTF-8 headers fine, but a strict
// RFC822 parser might not.
//
// ponytail: no multipart/attachment support. Callers must reject drafts
// with attachments before calling this — add multipart/mixed if a Linux
// account genuinely needs to send one.
func buildMIMEMessage(d *models.Draft, from string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(d.To, ", "))
	if len(d.CC) > 0 {
		fmt.Fprintf(&b, "Cc: %s\r\n", strings.Join(d.CC, ", "))
	}
	fmt.Fprintf(&b, "Subject: %s\r\n", d.Subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-Id: <%s@mailctl>\r\n", uuid.New().String())
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(d.Body)
	return []byte(b.String())
}

// sendViaSMTP dials host:port and sends msg, using STARTTLS if the server
// offers it — net/smtp.SendMail negotiates that automatically. Only
// PLAIN/LOGIN auth is supported (no implicit TLS on 465, no OAuth2 — v1
// scope cut, see the plan's Global Constraints).
func sendViaSMTP(host string, port int, username, password, from string, to []string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	auth := smtp.PlainAuth("", username, password, host)
	return smtp.SendMail(addr, auth, from, to, msg)
}
```

- [ ] **Step 4: Run the test again to confirm it passes**

Run: `go test ./internal/mail/... -run TestBuildMIMEMessage -v`
Expected: PASS

- [ ] **Step 5: Run the full test suite**

Run: `go build ./... && go test ./...`
Expected: builds clean, all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/mail/thunderbird_smtp.go internal/mail/thunderbird_smtp_test.go
git commit -m "mail: add MIME message builder and net/smtp send helper"
```

---

## Task 7: thunderbird.go — the linux-only public API

**Files:**
- Create: `internal/mail/thunderbird.go`

**Interfaces:**
- Consumes: `thunderbirdAccounts()`, `tbAccount` (Task 3); `parseMboxFile()` (Task 4); `keyring.GetPassword()` (Task 5); `buildMIMEMessage()`, `sendViaSMTP()` (Task 6); `models.Message`, `models.Draft` (existing).
- Produces: `FetchInbox`, `FetchMessageBody`, `SearchMessages`, `FetchThread`, `ListAccounts`, `Send`, `SaveDraft`, `MarkUnreadInMail`, `DeleteInMail`, `OpenInMail` — same names/signatures as `apple.go`, so every existing caller in `cmd/`, `internal/mcpserver`, `internal/tui` works unchanged when built for Linux.

This is the one file in this plan that carries `//go:build linux`, because it's the only one whose function names would collide with `apple.go`. Every real algorithm it needs (profile resolution, mbox parsing, MIME building, SMTP send) was already written and tested in Tasks 3, 4, 5, 6 on this darwin machine — this file is glue.

Reminder from the Global Constraints: `SaveDraft`, `MarkUnreadInMail`, `DeleteInMail`, and `OpenInMail` all return a fixed "not supported" error rather than writing to a live Thunderbird mbox file. `Send` rejects drafts with attachments.

**This task cannot be functionally tested on this darwin machine** — the file won't even be included in a darwin build (that's the point of the tag). Verification here is a cross-compile check only; treat this as unverified until it's run against a real Linux host with Thunderbird installed, and say so rather than claim it works.

- [ ] **Step 1: Implement thunderbird.go**

Create `internal/mail/thunderbird.go`:

```go
//go:build linux

package mail

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aeon022/mailctl/internal/keyring"
	"github.com/aeon022/mailctl/internal/models"
)

const sourceName = "thunderbird"

var errNotSupportedOnLinux = fmt.Errorf(
	"not supported on Linux — mailctl reads Thunderbird's mailbox files directly and won't write back into a file Thunderbird may have open")

func inboxPath(acc tbAccount) string {
	return filepath.Join(acc.Directory, "INBOX")
}

// FetchInbox returns recent message headers+bodies from every IMAP
// account's inbox in the default Thunderbird profile. Unlike apple.go,
// body is included here directly: parsing an mbox file is a cheap local
// read, not a round trip through AppleScript, so there's no reason to
// defer it to FetchMessageBody the way the Apple backend does.
func FetchInbox(count int, unreadOnly bool) ([]models.Message, error) {
	accounts, err := thunderbirdAccounts()
	if err != nil {
		return nil, err
	}
	var all []models.Message
	for _, acc := range accounts {
		msgs, err := parseMboxFile(inboxPath(acc), acc.Email, "INBOX", sourceName)
		if err != nil {
			continue // no INBOX file yet for this account — skip, not fatal
		}
		all = append(all, msgs...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Date.After(all[j].Date) })
	if unreadOnly {
		var unread []models.Message
		for _, m := range all {
			if !m.Read {
				unread = append(unread, m)
			}
		}
		all = unread
	}
	if len(all) > count {
		all = all[:count]
	}
	return all, nil
}

// FetchMessageBody re-scans the owning account's inbox for a message
// matching subject+from. account narrows the scan when known; empty
// scans every account.
func FetchMessageBody(account, subject, from string) (string, error) {
	accounts, err := thunderbirdAccounts()
	if err != nil {
		return "", err
	}
	for _, acc := range accounts {
		if account != "" && acc.Email != account {
			continue
		}
		msgs, err := parseMboxFile(inboxPath(acc), acc.Email, "INBOX", sourceName)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			if m.Subject == subject && strings.Contains(m.From, from) {
				return m.Body, nil
			}
		}
	}
	return "", fmt.Errorf("message not found: subject=%q from=%q", subject, from)
}

// SearchMessages scans every account's inbox for a subject match, mirroring
// apple.go's subject-only live search.
func SearchMessages(query string, count int) ([]models.Message, error) {
	accounts, err := thunderbirdAccounts()
	if err != nil {
		return nil, err
	}
	var out []models.Message
	q := strings.ToLower(query)
	for _, acc := range accounts {
		msgs, err := parseMboxFile(inboxPath(acc), acc.Email, "INBOX", sourceName)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			if strings.Contains(strings.ToLower(m.Subject), q) {
				out = append(out, m)
			}
		}
	}
	if len(out) > count {
		out = out[:count]
	}
	return out, nil
}

// FetchThread is the same subject-contains scan as SearchMessages — the
// Apple backend's two AppleScript blocks for this are likewise near-
// identical, just named for two different call sites.
func FetchThread(subject string, count int) ([]models.Message, error) {
	return SearchMessages(subject, count)
}

// ListAccounts returns every IMAP account's email address.
func ListAccounts() ([]string, error) {
	accounts, err := thunderbirdAccounts()
	if err != nil {
		return nil, err
	}
	out := make([]string, len(accounts))
	for i, a := range accounts {
		out[i] = a.Email
	}
	return out, nil
}

func pickSendingAccount(accounts []tbAccount, wantEmail string) (tbAccount, error) {
	if wantEmail == "" {
		if len(accounts) == 1 {
			return accounts[0], nil
		}
		return tbAccount{}, fmt.Errorf("multiple Thunderbird accounts found — specify an account in the draft's `account` field")
	}
	for _, a := range accounts {
		if a.Email == wantEmail {
			return a, nil
		}
	}
	return tbAccount{}, fmt.Errorf("no Thunderbird account found for %q", wantEmail)
}

// Send delivers d via the sending account's own SMTP server, authenticated
// with a password from the OS keyring.
func Send(d *models.Draft) error {
	if len(d.Attachments) > 0 {
		return fmt.Errorf("attachments are not supported when sending via the Linux/Thunderbird backend yet")
	}
	accounts, err := thunderbirdAccounts()
	if err != nil {
		return err
	}
	acc, err := pickSendingAccount(accounts, d.Account)
	if err != nil {
		return err
	}
	password, err := keyring.GetPassword(acc.Email)
	if err != nil {
		return fmt.Errorf("no password stored for %s — run: mailctl account set-password %s", acc.Email, acc.Email)
	}
	msg := buildMIMEMessage(d, acc.Email)
	return sendViaSMTP(acc.SMTPHost, acc.SMTPPort, acc.Username, password, acc.Email, d.To, msg)
}

// SaveDraft is not supported on Linux — see the plan's Global Constraints:
// writing into Thunderbird's Drafts mbox carries the same live-file
// corruption risk as MarkUnreadInMail/DeleteInMail.
func SaveDraft(d *models.Draft) error { return errNotSupportedOnLinux }

func MarkUnreadInMail(messageID string) error { return errNotSupportedOnLinux }
func DeleteInMail(messageID string) error     { return errNotSupportedOnLinux }
func OpenInMail(messageID string) error       { return errNotSupportedOnLinux }
```

- [ ] **Step 2: Cross-compile check**

Run: `GOOS=linux GOARCH=amd64 go build ./...`
Expected: builds clean. This confirms the code compiles for Linux — it does **not** confirm it behaves correctly against a real Thunderbird profile. Note that explicitly when reporting this task done.

- [ ] **Step 3: Confirm the darwin build is still unaffected**

Run: `go build ./... && go test ./...`
Expected: builds clean, all tests pass — `thunderbird.go` is invisible to this build (linux-only tag), so this is really just reconfirming Task 1–6 didn't regress.

- [ ] **Step 4: Commit**

```bash
git add internal/mail/thunderbird.go
git commit -m "mail: add linux-only Thunderbird backend implementing the mail package's public API"
```

---

## Task 8: `mailctl account set-password` command

**Files:**
- Create: `cmd/account.go`
- Modify: `go.mod`, `go.sum` (adds `golang.org/x/term`)

**Interfaces:**
- Consumes: `keyring.SetPassword(email, password string) error` (Task 5).
- Produces: the `mailctl account set-password <email>` CLI command.

New dependency: `golang.org/x/term` v0.45.0 — Go-team-maintained (not a third-party lib), the standard way to read a password from a terminal without echoing it. `cmd/` has no existing test files (checked: none exist today), so this task follows that convention — it's manually verified, not unit-tested, consistent with the rest of the package.

- [ ] **Step 1: Add the dependency**

Run: `go get golang.org/x/term@v0.45.0`
Expected: `go.mod`/`go.sum` updated.

- [ ] **Step 2: Implement cmd/account.go**

Create `cmd/account.go`:

```go
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/aeon022/mailctl/internal/keyring"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var accountCmd = &cobra.Command{
	Use:   "account",
	Short: "Manage per-account credentials",
}

var setPasswordCmd = &cobra.Command{
	Use:     "set-password <email>",
	Short:   "Store an SMTP app password for an account in the OS keyring",
	Example: `  mailctl account set-password jan@example.com`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		email := args[0]
		fmt.Printf("Password for %s: ", email)
		password, err := readPassword()
		fmt.Println()
		if err != nil {
			return err
		}
		if password == "" {
			return fmt.Errorf("password cannot be empty")
		}
		if err := keyring.SetPassword(email, password); err != nil {
			return fmt.Errorf("store password: %w", err)
		}
		fmt.Printf("Stored password for %s.\n", email)
		return nil
	},
}

// readPassword reads a password from stdin without echoing it when stdin
// is a real terminal; falls back to a plain line read (e.g. piped input in
// scripts) otherwise.
func readPassword() (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		return string(b), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

func init() {
	accountCmd.AddCommand(setPasswordCmd)
	rootCmd.AddCommand(accountCmd)
}
```

- [ ] **Step 3: Build and smoke-test manually**

Run: `go build ./... && go test ./...`
Expected: builds clean, all tests pass.

Then, manually: `echo "test-password" | ./mailctl account set-password test@example.com` (piped path, no terminal) followed by checking `mailctl` didn't crash and printed "Stored password for test@example.com." — this exercises the non-terminal fallback branch; the real `term.ReadPassword` interactive branch can only be verified by running the command directly in a terminal, which isn't scriptable here — note that when reporting this step.

- [ ] **Step 4: Commit**

```bash
git add cmd/account.go go.mod go.sum
git commit -m "cmd: add 'account set-password' for storing SMTP passwords in the OS keyring"
```

---

## Task 9: `mailctl doctor` — Linux-aware check

**Files:**
- Modify: `cmd/doctor.go`

**Interfaces:**
- Consumes: `mail.ThunderbirdProfileFound() (string, bool)` (Task 3).
- Produces: nothing new exported — `mailctl doctor`'s check list changes at runtime based on `runtime.GOOS`.

`doctor.CheckAppleApp` (from `missionctl-core`) shells out to `osascript`, which doesn't exist on Linux — it would just always report the check failed there. Branch the check list instead of calling it on Linux. No build tag needed on `doctor.go` itself: `mail.ThunderbirdProfileFound` lives in the untagged `thunderbird_profile.go` from Task 3, so it's callable regardless of which OS this is compiled for.

- [ ] **Step 1: Update cmd/doctor.go**

Replace the full contents of `cmd/doctor.go`:

```go
package cmd

import (
	"os"
	"runtime"

	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/mailctl/internal/mail"
	"github.com/aeon022/missionctl-core/doctor"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check config, database, and mail client health",
	Run: func(cmd *cobra.Command, args []string) {
		checks := []doctor.Check{
			doctor.CheckSQLite("Database", config.DBPath(), "messages"),
			doctor.CheckDataDir("Data directory", config.DBPath(), config.Shared()),
		}
		if runtime.GOOS == "linux" {
			if dir, ok := mail.ThunderbirdProfileFound(); ok {
				checks = append(checks, doctor.Check{Label: "Thunderbird profile", OK: true, Detail: dir})
			} else {
				checks = append(checks, doctor.Check{Label: "Thunderbird profile", OK: false, Detail: "no default profile found under ~/.thunderbird"})
			}
		} else {
			checks = append(checks, doctor.CheckAppleApp("Mail.app", "Mail"))
		}
		if !doctor.PrintReport(checks) {
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
```

- [ ] **Step 2: Build and run on darwin**

Run: `go build ./... && ./mailctl doctor`
Expected: same output as before this change (darwin branch is unchanged behavior, just reached via the new `else`).

- [ ] **Step 3: Cross-compile check for the Linux branch**

Run: `GOOS=linux GOARCH=amd64 go build ./...`
Expected: builds clean.

- [ ] **Step 4: Run the full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/doctor.go
git commit -m "cmd: doctor checks Thunderbird profile on Linux instead of Mail.app"
```

---

## Task 10: Docs — README and setup.sh

**Files:**
- Modify: `README.md`
- Modify: `setup.sh`

**Interfaces:**
- Consumes: nothing.
- Produces: nothing code-facing — documentation and install-script correctness only.

`setup.sh` currently always checks Apple Mail access via `osascript`, which doesn't exist on Linux — running it there today prints a spurious "Warning: Could not access Apple Mail" on every install. `README.md`'s opening line ("Terminal email client for Apple Mail") and Quick Start are macOS-only.

- [ ] **Step 1: Make setup.sh Linux-aware**

In `setup.sh`, replace:

```bash
echo "Checking Apple Mail access..."
if ! osascript -e 'tell application "Mail" to return name of accounts' &>/dev/null 2>&1; then
    echo ""
    echo "Warning: Could not access Apple Mail."
    echo "Make sure Apple Mail is open and has at least one account configured."
    echo "You may need to grant Automation permissions:"
    echo "  System Settings → Privacy & Security → Automation → Terminal → Mail ✓"
    echo ""
fi
```

with:

```bash
if [ "$(uname)" = "Darwin" ]; then
    echo "Checking Apple Mail access..."
    if ! osascript -e 'tell application "Mail" to return name of accounts' &>/dev/null 2>&1; then
        echo ""
        echo "Warning: Could not access Apple Mail."
        echo "Make sure Apple Mail is open and has at least one account configured."
        echo "You may need to grant Automation permissions:"
        echo "  System Settings → Privacy & Security → Automation → Terminal → Mail ✓"
        echo ""
    fi
else
    echo "Checking for a Thunderbird profile..."
    if [ ! -f "$HOME/.thunderbird/profiles.ini" ]; then
        echo ""
        echo "Warning: No Thunderbird profile found at ~/.thunderbird/profiles.ini."
        echo "Install and run Thunderbird at least once, with one account configured,"
        echo "before running mailctl sync."
        echo ""
    fi
    echo ""
    echo "To send mail, store your account's SMTP app password:"
    echo "  mailctl account set-password you@example.com"
fi
```

- [ ] **Step 2: Update README.md's opening description and add a Linux section**

Change the opening line from:
```
Terminal email client for Apple Mail. Part of the [missionctl](https://github.com/aeon022/missionctl) suite.
```
to:
```
Terminal email client for Apple Mail (macOS) or Thunderbird (Linux). Part of the [missionctl](https://github.com/aeon022/missionctl) suite.
```

Add a new section after the existing "Quick Start" section:

```markdown
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
```

- [ ] **Step 3: Commit**

```bash
git add README.md setup.sh
git commit -m "docs: document Linux/Thunderbird support and its current limits"
```

---

## Manual Verification Checklist (not automatable from this darwin machine)

After all tasks land, these need a real Linux host with Thunderbird installed and at least one IMAP account configured — call this out plainly rather than claiming the feature works end-to-end without it:

- [ ] `mailctl doctor` reports the Thunderbird profile found
- [ ] `mailctl sync` populates the local cache from the real mbox file
- [ ] `mailctl inbox` / the TUI show real messages, correct read/unread state
- [ ] `mailctl account set-password you@example.com` stores a password (verify via the desktop's keyring app, e.g. Seahorse on GNOME)
- [ ] `mailctl send draft.md` actually delivers mail via the account's SMTP server
- [ ] Deleting/marking-unread/opening from the TUI surfaces the "not supported on Linux" error instead of silently doing nothing or crashing
