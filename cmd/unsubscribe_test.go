package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/mailctl/internal/unsub"
)

type recorder struct {
	done, drafted []unsub.Target
	lu, post      string
	headersErr    error
	draftErr      error
}

func (r *recorder) actions() unsubActions {
	return unsubActions{
		headers: func(_, _, _ string) (string, string, error) { return r.lu, r.post, r.headersErr },
		do:      func(t unsub.Target) error { r.done = append(r.done, t); return nil },
		draft:   func(t unsub.Target) error { r.drafted = append(r.drafted, t); return r.draftErr },
	}
}

var testMsg = models.Message{ID: "n1", Subject: "Weekly", From: "News <news@list.test>"}

func TestUnsubscribeAsksAndCancelsByDefault(t *testing.T) {
	r := &recorder{lu: "<https://list.test/u>", post: "List-Unsubscribe=One-Click"}
	var out bytes.Buffer
	err := unsubscribeFlow(&out, strings.NewReader("n\n"), true, testMsg, false, r.actions())
	if err != nil || len(r.done) != 0 || len(r.drafted) != 0 {
		t.Fatalf("declined: err=%v done=%d drafted=%d", err, len(r.done), len(r.drafted))
	}
	for _, want := range []string{"News <news@list.test>", "one-click unsubscribe request to list.test", "Proceed? [y/N]", "Cancelled"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestUnsubscribeConfirmedOneClick(t *testing.T) {
	r := &recorder{lu: "<mailto:u@list.test>, <https://list.test/u>", post: "List-Unsubscribe=One-Click"}
	var out bytes.Buffer
	if err := unsubscribeFlow(&out, strings.NewReader("y\n"), true, testMsg, false, r.actions()); err != nil {
		t.Fatal(err)
	}
	if len(r.done) != 1 || r.done[0].Kind != unsub.OneClick || len(r.drafted) != 0 {
		t.Errorf("done=%+v drafted=%+v", r.done, r.drafted)
	}
	if !strings.Contains(out.String(), "accepted") {
		t.Errorf("output: %s", out.String())
	}
}

func TestUnsubscribeYesSkipsPromptAndMailtoOnlyDrafts(t *testing.T) {
	r := &recorder{lu: "<mailto:unsub@list.test?subject=bye>"}
	var out bytes.Buffer
	if err := unsubscribeFlow(&out, strings.NewReader(""), false, testMsg, true, r.actions()); err != nil {
		t.Fatal(err)
	}
	if len(r.drafted) != 1 || r.drafted[0].To != "unsub@list.test" || r.drafted[0].Subject != "bye" || len(r.done) != 0 {
		t.Errorf("mailto must only draft: drafted=%+v done=%+v", r.drafted, r.done)
	}
	if strings.Contains(out.String(), "Proceed?") || !strings.Contains(out.String(), "Draft saved") {
		t.Errorf("output: %s", out.String())
	}
}

func TestUnsubscribeMailtoDraftUnavailablePrintsManualInstructions(t *testing.T) {
	r := &recorder{lu: "<mailto:unsub@list.test>", draftErr: errors.New("not supported on Linux")}
	var out bytes.Buffer
	if err := unsubscribeFlow(&out, strings.NewReader(""), false, testMsg, true, r.actions()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "To:      unsub@list.test") || !strings.Contains(out.String(), "Couldn't save a draft") {
		t.Errorf("output: %s", out.String())
	}
}

func TestUnsubscribeRefusesWithoutTerminalOrHeader(t *testing.T) {
	r := &recorder{lu: "<https://list.test/u>"}
	var out bytes.Buffer
	if err := unsubscribeFlow(&out, strings.NewReader("y\n"), false, testMsg, false, r.actions()); err == nil || len(r.done) != 0 {
		t.Errorf("piped stdin without --yes must refuse: err=%v done=%d", err, len(r.done))
	}
	none := &recorder{}
	if err := unsubscribeFlow(&out, strings.NewReader(""), true, testMsg, true, none.actions()); err == nil || !strings.Contains(err.Error(), "no List-Unsubscribe") {
		t.Errorf("no header: %v", err)
	}
	bad := &recorder{headersErr: errors.New("mail not running")}
	if err := unsubscribeFlow(&out, strings.NewReader(""), true, testMsg, true, bad.actions()); err == nil || !strings.Contains(err.Error(), "mail not running") {
		t.Errorf("header lookup error must surface: %v", err)
	}
}
