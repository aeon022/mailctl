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
