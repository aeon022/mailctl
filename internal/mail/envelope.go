//go:build darwin

package mail

import (
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aeon022/mailctl/internal/models"
	_ "modernc.org/sqlite"
)

// Search and thread lookups read a private copy of Mail.app's Envelope Index
// (read-only) instead of driving Mail.app through AppleScript — which was 6s to
// minutes per query and launches the Apple app. The copy (db + WAL) is taken per
// call so Mail.app's open file is never touched and the WAL is replayed in our
// copy. The index holds subject, sender and a short summary, not full bodies.
// ponytail: ~120 MB copy per call (~0.1s); cache by mtime if it ever matters.

// envelopeDir is a var so tests can point at a fixture.
var envelopeDir = func() string {
	m, _ := filepath.Glob(filepath.Join(os.Getenv("HOME"), "Library/Mail/V*/MailData"))
	if len(m) == 0 {
		return ""
	}
	return m[len(m)-1]
}

func queryEnvelope(where string, arg string, count int) ([]models.Message, error) {
	src := envelopeDir()
	if src == "" {
		return nil, fmt.Errorf("Mail envelope index not found (Full Disk Access for the terminal may be missing)")
	}
	tmp, err := os.MkdirTemp("", "mailctl-ei-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	for _, f := range []string{"Envelope Index", "Envelope Index-wal"} {
		if err := copyFile(filepath.Join(src, f), filepath.Join(tmp, f)); err != nil && f == "Envelope Index" {
			return nil, fmt.Errorf("copy envelope index: %w", err)
		}
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(tmp, "Envelope Index")+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	args := make([]any, 0, 5)
	for range strings.Count(where, "?") {
		args = append(args, arg)
	}
	args = append(args, count)
	rows, err := db.Query(`
SELECT m.ROWID, s.subject, COALESCE(a.comment,''), COALESCE(a.address,''),
       m.date_received, m.read, mb.url, m.conversation_id
FROM messages m
JOIN subjects s ON s.ROWID = m.subject
JOIN mailboxes mb ON mb.ROWID = m.mailbox
LEFT JOIN addresses a ON a.ROWID = m.sender
LEFT JOIN summaries su ON su.ROWID = m.summary
WHERE m.deleted = 0 AND (`+where+`)
ORDER BY m.date_received DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Message
	for rows.Next() {
		var (
			id, ts, conv   int64
			read           int
			subj, name, ad string
			mbURL          string
		)
		if err := rows.Scan(&id, &subj, &name, &ad, &ts, &read, &mbURL, &conv); err != nil {
			return nil, err
		}
		from := ad
		if name != "" {
			from = name + " <" + ad + ">"
		}
		acct, box := splitMailboxURL(mbURL)
		out = append(out, models.Message{
			ID: fmt.Sprint(id), Subject: subj, From: from, Date: time.Unix(ts, 0),
			Read: read == 1, Mailbox: box, Account: acct, Source: sourceName,
			ThreadID: fmt.Sprint(conv),
		})
	}
	return out, rows.Err()
}

// splitMailboxURL turns "ews://UUID/Posteingang/Buchhaltung" into ("UUID-prefix", "Posteingang/Buchhaltung").
func splitMailboxURL(u string) (account, box string) {
	p, err := url.Parse(u)
	if err != nil {
		return "", u
	}
	b, _ := url.PathUnescape(strings.TrimPrefix(p.Path, "/"))
	return p.Scheme + ":" + p.Host[:min(8, len(p.Host))], b
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
