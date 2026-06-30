package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/aeon022/mailctl/internal/models"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_journal=WAL&_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	s := &Store{db: db}
	return s, s.migrate()
}

func (s *Store) Close() error { return s.db.Close() }

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
