package mcpserver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/missionctl-core/activity"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleSendLogsSubjectOnSuccessOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MISSIONCTL_DATA_DIR", t.TempDir())
	t.Setenv("MISSIONCTL_ACTIVITY", "")
	orig := sendMail
	t.Cleanup(func() { sendMail = orig })

	req := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
		"to": "x@y.test", "subject": "Angebot", "body": "vertraulich"}}}
	logged := func() []activity.Event {
		from, to := activity.Day(time.Now())
		evs, _ := activity.Read(from, to)
		return evs
	}

	sendMail = func(*models.Draft) error { return errors.New("down") }
	if res, _ := handleSend(context.Background(), req); !res.IsError || len(logged()) != 0 {
		t.Fatalf("failed send: isError=%v logged=%d", res.IsError, len(logged()))
	}
	sendMail = func(*models.Draft) error { return nil }
	if res, _ := handleSend(context.Background(), req); res.IsError {
		t.Fatal("send failed")
	}
	evs := logged()
	if len(evs) != 1 || evs[0].Action != "sent" || evs[0].Title != "Angebot" {
		t.Fatalf("events = %+v", evs)
	}
}
