package mail

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testMbox mirrors what Thunderbird actually writes: RFC 2047 encoded-word
// headers, quoted-printable and multipart/alternative bodies, and deleted
// messages still sitting in the file because the user never compacted.
const testMbox = `From MAILER-DAEMON Mon Jan  5 09:00:00 2026
From: Jan <jan@example.com>
To: me@example.com
Subject: First message
Date: Mon, 5 Jan 2026 09:00:00 +0000
Message-Id: <msg1@example.com>
X-Mozilla-Status: 0001

Hi, this is the first message.
>From the escaping convention, this line starts with >From but isn't a real envelope.
It should survive intact in the parsed body.

From MAILER-DAEMON Tue Jan  6 10:00:00 2026
From: Lisa <lisa@example.com>
To: me@example.com
Subject: Second message
Date: Tue, 6 Jan 2026 10:00:00 +0000
Message-Id: <msg2@example.com>
X-Mozilla-Status: 0000

Hi, this is the second, unread message.

From MAILER-DAEMON Wed Jan  7 11:00:00 2026
From: =?UTF-8?B?SsO2cmcgTcO8bGxlcg==?= <joerg@example.com>
To: me@example.com
Subject: =?UTF-8?B?VmVydHJhZyDDvGJlciAxMDAg4oKs?=
Date: Wed, 7 Jan 2026 11:00:00 +0000
Message-Id: <msg3@example.com>
X-Mozilla-Status: 0000
MIME-Version: 1.0
Content-Type: text/plain; charset=UTF-8
Content-Transfer-Encoding: quoted-printable

Anbei die Rechnung =C3=BCber 100 =E2=82=AC f=
=C3=BCr die Beratung.

From MAILER-DAEMON Thu Jan  8 12:00:00 2026
From: Team <team@example.com>
To: me@example.com
Subject: =?UTF-8?Q?Gr=C3=BC=C3=9Fe_vom_B=C3=BCro?=
Date: Thu, 8 Jan 2026 12:00:00 +0000
Message-Id: <msg4@example.com>
X-Mozilla-Status: 0000
MIME-Version: 1.0
Content-Type: multipart/alternative; boundary="----=_Part_42"

This is a multi-part message in MIME format.
------=_Part_42
Content-Type: text/plain; charset=UTF-8
Content-Transfer-Encoding: quoted-printable

Die Sitzung f=C3=A4llt aus.
------=_Part_42
Content-Type: text/html; charset=UTF-8

<html><body><p>Die Sitzung faellt aus.</p></body></html>
------=_Part_42--

From MAILER-DAEMON Fri Jan  9 13:00:00 2026
From: Spam <spam@example.com>
To: me@example.com
Subject: Deleted but not compacted
Date: Fri, 9 Jan 2026 13:00:00 +0000
Message-Id: <msg5@example.com>
X-Mozilla-Status: 0009

The user deleted this in Thunderbird; the bytes stay until Compact Folders.

From MAILER-DAEMON Sat Jan 10 14:00:00 2026
From: Spam2 <spam2@example.com>
To: me@example.com
Subject: Deleted on the IMAP side
Date: Sat, 10 Jan 2026 14:00:00 +0000
Message-Id: <msg6@example.com>
X-Mozilla-Status: 0001
X-Mozilla-Status2: 00200000

Deleted from another client; Thunderbird recorded it in Status2.
`

func TestSplitMbox(t *testing.T) {
	parts := splitMbox([]byte(testMbox))
	if len(parts) != 6 {
		t.Fatalf("got %d messages, want 6", len(parts))
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
	// Verify ">From " escaping edge case: line starting with >From at column 0
	// should survive intact in body, not be treated as an envelope delimiter
	if !strings.Contains(m1.Body, ">From the escaping convention") {
		t.Errorf("Body missing escaped >From line: %q", m1.Body)
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

// RFC 2047 encoded-words: net/mail's Header.Get returns them raw, so any
// German-language subject/sender would otherwise land in the store as
// "=?UTF-8?B?...?=" and break substring search.
func TestParseMboxMessage_DecodesEncodedWords(t *testing.T) {
	parts := splitMbox([]byte(testMbox))
	m, err := parseMboxMessage(parts[2], "jan@example.com", "INBOX", "thunderbird")
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "Vertrag über 100 €" {
		t.Errorf("Subject = %q, want decoded B-encoded word", m.Subject)
	}
	if m.From != "Jörg Müller <joerg@example.com>" {
		t.Errorf("From = %q, want decoded B-encoded word", m.From)
	}

	q, err := parseMboxMessage(parts[3], "jan@example.com", "INBOX", "thunderbird")
	if err != nil {
		t.Fatal(err)
	}
	if q.Subject != "Grüße vom Büro" {
		t.Errorf("Subject = %q, want decoded Q-encoded word", q.Subject)
	}
}

func TestParseMboxMessage_DecodesQuotedPrintableBody(t *testing.T) {
	parts := splitMbox([]byte(testMbox))
	m, err := parseMboxMessage(parts[2], "jan@example.com", "INBOX", "thunderbird")
	if err != nil {
		t.Fatal(err)
	}
	if m.Body != "Anbei die Rechnung über 100 € für die Beratung." {
		t.Errorf("Body = %q, want quoted-printable decoded (incl. soft line break)", m.Body)
	}
}

func TestParseMboxMessage_ExtractsPlainPartOfMultipart(t *testing.T) {
	parts := splitMbox([]byte(testMbox))
	m, err := parseMboxMessage(parts[3], "jan@example.com", "INBOX", "thunderbird")
	if err != nil {
		t.Fatal(err)
	}
	if m.Body != "Die Sitzung fällt aus." {
		t.Errorf("Body = %q, want the text/plain part only", m.Body)
	}
	if strings.Contains(m.Body, "<html>") || strings.Contains(m.Body, "_Part_42") {
		t.Errorf("Body leaks HTML part or boundary markers: %q", m.Body)
	}
}

func TestParseMboxMessage_SkipsExpunged(t *testing.T) {
	parts := splitMbox([]byte(testMbox))
	for i, want := range map[int]string{4: "X-Mozilla-Status 0x0008", 5: "X-Mozilla-Status2 IMAPDeleted"} {
		if _, err := parseMboxMessage(parts[i], "jan@example.com", "INBOX", "thunderbird"); !errors.Is(err, errMessageExpunged) {
			t.Errorf("part %d (%s): err = %v, want errMessageExpunged", i, want, err)
		}
	}
}

// mimeText falls back to the HTML part when a multipart/alternative has no
// text/plain — better a readable-ish blob than an empty message.
func TestMimeText_FallsBackToHTMLPart(t *testing.T) {
	raw := "------=_P\n" +
		"Content-Type: text/html; charset=UTF-8\n\n" +
		"<p>only html here</p>\n" +
		"------=_P--\n"
	got := mimeText(`multipart/alternative; boundary="----=_P"`, "", []byte(raw))
	if !strings.Contains(got, "only html here") {
		t.Errorf("got %q, want the html part as fallback", got)
	}
}

func TestDecodeTransferEncoding_Base64(t *testing.T) {
	if got := decodeTransferEncoding("base64", []byte("SGFsbG8gV2VsdA==")); got != "Hallo Welt" {
		t.Errorf("got %q", got)
	}
	// Undecodable input degrades to raw rather than losing the message.
	if got := decodeTransferEncoding("base64", []byte("!!!not base64!!!")); got != "!!!not base64!!!" {
		t.Errorf("got %q, want raw fallback", got)
	}
}

func TestParseMboxFile_DropsExpungedMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "INBOX")
	if err := os.WriteFile(path, []byte(testMbox), 0o600); err != nil {
		t.Fatal(err)
	}
	msgs, err := parseMboxFile(path, "jan@example.com", "INBOX", "thunderbird")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 {
		t.Fatalf("got %d messages, want 4 (both deleted ones dropped)", len(msgs))
	}
	for _, m := range msgs {
		if strings.HasPrefix(m.Subject, "Deleted") {
			t.Errorf("expunged message resurrected: %q", m.Subject)
		}
	}
}
