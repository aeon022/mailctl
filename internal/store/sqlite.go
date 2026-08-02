package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/missionctl-core/syncdir"
	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	path string
}

// mailctl opens a fresh *Store per operation rather than holding one open
// for the process's lifetime, and flock(2) isn't reentrant within a
// process — locks reference-counts the real OS-level lock per path so the
// same process's own concurrent/sequential opens don't conflict with
// themselves; only the first open of a path acquires it for real, and only
// the last matching Close() releases it. A conflict is reported only when
// a genuinely different process holds it.
var (
	lockMu sync.Mutex
	locks  = map[string]*lockEntry{}
)

type lockEntry struct {
	lock  *syncdir.Lock
	count int
}

func acquireLock(path string) error {
	lockMu.Lock()
	defer lockMu.Unlock()
	e, ok := locks[path]
	if !ok {
		l, err := syncdir.Acquire(path)
		if err != nil {
			return err
		}
		e = &lockEntry{lock: l}
		locks[path] = e
	}
	e.count++
	return nil
}

func releaseLock(path string) {
	lockMu.Lock()
	defer lockMu.Unlock()
	e, ok := locks[path]
	if !ok {
		return
	}
	e.count--
	if e.count == 0 {
		e.lock.Release()
		delete(locks, path)
	}
}

// New opens the database at path. shared must reflect whether path is a
// user-configured (possibly folder-synced) directory rather than the
// tool's private default — see config.Shared.
func New(path string, shared bool) (*Store, error) {
	if isPlaceholder, placeholder := syncdir.ICloudPlaceholder(path); isPlaceholder {
		return nil, fmt.Errorf("%s hasn't finished downloading from iCloud yet (found %s) — open Finder and download it, or disable \"Optimize Mac Storage\" for this folder", path, placeholder)
	}

	if err := acquireLock(path); err != nil {
		if errors.Is(err, syncdir.ErrLocked) {
			return nil, fmt.Errorf("mailctl is already running elsewhere, or a previous session crashed — remove %s.lock if you're sure nothing else is using it", path)
		}
		return nil, err
	}

	db, err := sql.Open("sqlite", path+"?_journal="+syncdir.JournalMode(shared)+"&_timeout=5000")
	if err != nil {
		releaseLock(path)
		return nil, fmt.Errorf("open db: %w", err)
	}
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		releaseLock(path)
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	err := s.db.Close()
	releaseLock(s.path)
	return err
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id         TEXT PRIMARY KEY,
			subject    TEXT NOT NULL DEFAULT '',
			from_addr  TEXT NOT NULL DEFAULT '',
			to_addrs   TEXT NOT NULL DEFAULT '',
			cc_addrs   TEXT NOT NULL DEFAULT '',
			body       TEXT NOT NULL DEFAULT '',
			date       TEXT NOT NULL,
			read       INTEGER NOT NULL DEFAULT 0,
			mailbox    TEXT NOT NULL DEFAULT '',
			account    TEXT NOT NULL DEFAULT '',
			thread_id  TEXT NOT NULL DEFAULT '',
			source     TEXT NOT NULL DEFAULT 'apple',
			synced_at  TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_messages_date    ON messages(date);
		CREATE INDEX IF NOT EXISTS idx_messages_read    ON messages(read);
		CREATE INDEX IF NOT EXISTS idx_messages_account ON messages(account);
		CREATE INDEX IF NOT EXISTS idx_messages_subject ON messages(subject);
	`)
	return err
}

func (s *Store) UpsertMessage(ctx context.Context, m *models.Message) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO messages (id,subject,from_addr,to_addrs,cc_addrs,body,date,read,mailbox,account,thread_id,source,synced_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			subject=excluded.subject, body=excluded.body,
			read=excluded.read, synced_at=excluded.synced_at
	`,
		m.ID, m.Subject, m.From,
		strings.Join(m.To, ","),
		strings.Join(m.CC, ","),
		m.Body,
		m.Date.UTC().Format(time.RFC3339),
		boolInt(m.Read),
		m.Mailbox, m.Account, m.ThreadID, m.Source,
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

type Filter struct {
	Account  string
	Mailbox  string
	UnreadOnly bool
	Query    string
	Limit    int
}

func (s *Store) ListMessages(ctx context.Context, f Filter) ([]models.Message, error) {
	q := `SELECT id,subject,from_addr,to_addrs,cc_addrs,body,date,read,mailbox,account,thread_id,source
		  FROM messages WHERE 1=1`
	var args []any
	if f.Account != "" {
		q += ` AND account=?`
		args = append(args, f.Account)
	}
	if f.Mailbox != "" {
		q += ` AND mailbox=?`
		args = append(args, f.Mailbox)
	}
	if f.UnreadOnly {
		q += ` AND read=0`
	}
	if f.Query != "" {
		q += ` AND (subject LIKE ? OR from_addr LIKE ? OR body LIKE ?)`
		like := "%" + f.Query + "%"
		args = append(args, like, like, like)
	}
	q += ` ORDER BY date DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (s *Store) ListAccounts(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT account FROM messages WHERE account != '' ORDER BY account`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

func (s *Store) MarkRead(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET read=1 WHERE id=?`, id)
	return err
}

func (s *Store) MarkUnread(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET read=0 WHERE id=?`, id)
	return err
}

func (s *Store) DeleteMessage(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE id=?`, id)
	return err
}

// UnreadCounts returns unread message counts per account plus "" for the total.
func (s *Store) UnreadCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT account, COUNT(*) FROM messages WHERE read=0 GROUP BY account`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	total := 0
	for rows.Next() {
		var acct string
		var n int
		if err := rows.Scan(&acct, &n); err != nil {
			return nil, err
		}
		counts[acct] = n
		total += n
	}
	counts[""] = total // "" = Alle
	return counts, rows.Err()
}

func (s *Store) DeleteBySource(ctx context.Context, source string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE source=?`, source)
	return err
}

func scanMessages(rows *sql.Rows) ([]models.Message, error) {
	var msgs []models.Message
	for rows.Next() {
		var m models.Message
		var dateStr string
		var toStr, ccStr string
		if err := rows.Scan(
			&m.ID, &m.Subject, &m.From, &toStr, &ccStr,
			&m.Body, &dateStr, &m.Read, &m.Mailbox, &m.Account, &m.ThreadID, &m.Source,
		); err != nil {
			return nil, err
		}
		m.Date, _ = time.Parse(time.RFC3339, dateStr)
		if toStr != "" {
			m.To = strings.Split(toStr, ",")
		}
		if ccStr != "" {
			m.CC = strings.Split(ccStr, ",")
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
