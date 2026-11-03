//go:build darwin

package mail

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestQueryEnvelope(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "Envelope Index"))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE subjects(ROWID INTEGER PRIMARY KEY, subject TEXT)`,
		`CREATE TABLE summaries(ROWID INTEGER PRIMARY KEY, summary TEXT)`,
		`CREATE TABLE addresses(ROWID INTEGER PRIMARY KEY, address TEXT, comment TEXT)`,
		`CREATE TABLE mailboxes(ROWID INTEGER PRIMARY KEY, url TEXT)`,
		`CREATE TABLE messages(ROWID INTEGER PRIMARY KEY, subject INT, summary INT, sender INT, mailbox INT, date_received INT, read INT, deleted INT, conversation_id INT)`,
		`INSERT INTO subjects VALUES (1,'Rechnung Oktober')`,
		`INSERT INTO summaries VALUES (1,'Anbei die Dienstleistung')`,
		`INSERT INTO addresses VALUES (1,'a@b.at','Anna')`,
		`INSERT INTO mailboxes VALUES (1,'ews://ABCDEFGH-1/Posteingang/Buchhaltung')`,
		`INSERT INTO messages VALUES (1,1,1,1,1,1700000000,1,0,7), (2,1,1,1,1,1700000001,1,1,7)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	envelopeDir = func() string { return dir }

	for _, tc := range []struct{ where, arg string }{
		{"s.subject LIKE '%'||?||'%'", "rechnung"},
		{"a.comment LIKE '%'||?||'%' OR su.summary LIKE '%'||?||'%'", "dienstleistung"},
	} {
		got, err := queryEnvelope(tc.where, tc.arg, 10)
		if err != nil || len(got) != 1 { // deleted row excluded
			t.Fatalf("%q: got %d, err %v", tc.arg, len(got), err)
		}
		if got[0].From != "Anna <a@b.at>" || got[0].Mailbox != "Posteingang/Buchhaltung" || !got[0].Read {
			t.Fatalf("bad row: %+v", got[0])
		}
	}
}
