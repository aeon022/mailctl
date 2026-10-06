package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/mailctl/internal/unsub"
	"github.com/aeon022/missionctl-core/activity"
)

func actSandbox(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MISSIONCTL_DATA_DIR", t.TempDir())
	t.Setenv("MISSIONCTL_ACTIVITY", "")
}

func loggedToday(t *testing.T) []activity.Event {
	t.Helper()
	from, to := activity.Day(time.Now())
	evs, err := activity.Read(from, to)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func TestUnsubscribeLogsSenderNameOnlyAndOnlyWhenPerformed(t *testing.T) {
	actSandbox(t)
	r := &recorder{lu: "<https://list.test/u>", post: "List-Unsubscribe=One-Click"}
	var out bytes.Buffer
	if err := unsubscribeFlow(&out, strings.NewReader(""), false, testMsg, true, r.actions()); err != nil {
		t.Fatal(err)
	}
	evs := loggedToday(t)
	if len(evs) != 1 || evs[0].Tool != "mailctl" || evs[0].Action != "unsubscribed" || evs[0].Title != "News" {
		t.Fatalf("events = %+v (display name only, never the address)", evs)
	}

	// declined, mailto-draft and failed attempts are not "unsubscribed"
	for name, run := range map[string]func() error{
		"declined": func() error {
			return unsubscribeFlow(&out, strings.NewReader("n\n"), true, testMsg, false, r.actions())
		},
		"mailto": func() error {
			m := &recorder{lu: "<mailto:u@list.test>"}
			return unsubscribeFlow(&out, strings.NewReader(""), false, testMsg, true, m.actions())
		},
		"failed": func() error {
			a := r.actions()
			a.do = func(unsub.Target) error { return errors.New("boom") }
			return unsubscribeFlow(&out, strings.NewReader(""), false, testMsg, true, a)
		},
	} {
		_ = run()
		if n := len(loggedToday(t)); n != 1 {
			t.Errorf("%s: logged %d events, still want 1", name, n)
		}
	}
}

func TestSendCommandLogsSubjectOnSuccessOnly(t *testing.T) {
	actSandbox(t)
	orig := sendMail
	t.Cleanup(func() { sendMail = orig })

	draft := filepath.Join(t.TempDir(), "mail.md")
	body := "---\nto:\n  - someone@example.test\nsubject: Quartalsbericht\n---\n\nText, der nie geloggt wird.\n"
	if err := os.WriteFile(draft, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	sendMail = func(*models.Draft) error { return errors.New("smtp down") }
	if err := sendCmd.RunE(sendCmd, []string{draft}); err == nil {
		t.Fatal("expected the send error to surface")
	}
	if n := len(loggedToday(t)); n != 0 {
		t.Fatalf("a failed send logged %d events", n)
	}

	sendMail = func(*models.Draft) error { return nil }
	if err := sendCmd.RunE(sendCmd, []string{draft}); err != nil {
		t.Fatal(err)
	}
	evs := loggedToday(t)
	if len(evs) != 1 || evs[0].Action != "sent" || evs[0].Title != "Quartalsbericht" {
		t.Fatalf("events = %+v", evs)
	}
}

func TestLoggingOffStillSends(t *testing.T) {
	actSandbox(t)
	t.Setenv("MISSIONCTL_ACTIVITY", "off")
	orig := sendMail
	t.Cleanup(func() { sendMail = orig })
	sent := 0
	sendMail = func(*models.Draft) error { sent++; return nil }

	draft := filepath.Join(t.TempDir(), "mail.md")
	if err := os.WriteFile(draft, []byte("---\nto:\n  - a@b.test\nsubject: Aus\n---\n\nx\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sendCmd.RunE(sendCmd, []string{draft}); err != nil || sent != 1 {
		t.Fatalf("send with logging off: err=%v sent=%d", err, sent)
	}
	if n := len(loggedToday(t)); n != 0 {
		t.Errorf("logged %d events while off", n)
	}
}
