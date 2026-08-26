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
