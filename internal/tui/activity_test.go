package tui

import (
	"errors"
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

func TestSendCmdLogsSubjectOnSuccessOnly(t *testing.T) {
	actSandbox(t)
	orig := sendMail
	t.Cleanup(func() { sendMail = orig })

	sendMail = func(*models.Draft) error { return errors.New("no smtp") }
	if msg := sendCmd("a@b.test", "Hallo", "geheimer Text", nil)(); msg.(sentMsg).err == nil {
		t.Fatal("send error must reach the model")
	}
	if n := len(loggedToday(t)); n != 0 {
		t.Fatalf("failed send logged %d events", n)
	}
	sendMail = func(*models.Draft) error { return nil }
	sendCmd("a@b.test", "Hallo", "geheimer Text", nil)()
	evs := loggedToday(t)
	if len(evs) != 1 || evs[0].Action != "sent" || evs[0].Title != "Hallo" {
		t.Fatalf("events = %+v", evs)
	}
}

func TestDoUnsubCmdLogsSenderNameAfterSuccess(t *testing.T) {
	actSandbox(t)
	orig := performUnsub
	t.Cleanup(func() { performUnsub = orig })

	performUnsub = func(unsub.Target) error { return errors.New("403") }
	doUnsubCmd(unsub.Target{Kind: unsub.OneClick, URL: "https://list.test/u"}, "News <news@list.test>")()
	if n := len(loggedToday(t)); n != 0 {
		t.Fatalf("failed unsubscribe logged %d events", n)
	}
	performUnsub = func(unsub.Target) error { return nil }
	doUnsubCmd(unsub.Target{Kind: unsub.OneClick, URL: "https://list.test/u"}, "News <news@list.test>")()
	evs := loggedToday(t)
	if len(evs) != 1 || evs[0].Action != "unsubscribed" || evs[0].Title != "News" {
		t.Fatalf("events = %+v", evs)
	}
}
