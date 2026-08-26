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
	if strings.Contains(msg, "Bcc:") {
		t.Errorf("Bcc must not appear in the headers, got:\n%s", msg)
	}
}

// The SMTP envelope decides delivery, not the Cc:/Bcc: headers — CC'd and
// BCC'd people got nothing before this was in the RCPT TO list.
func TestEnvelopeRecipients(t *testing.T) {
	d := &models.Draft{
		To:  []string{"jan@example.com", " "},
		CC:  []string{"lisa@example.com", "jan@example.com"},
		BCC: []string{" bob@example.com "},
	}
	got := envelopeRecipients(d)
	want := []string{"jan@example.com", "lisa@example.com", "bob@example.com"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
