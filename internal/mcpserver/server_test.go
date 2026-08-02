package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/mailctl/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
)

// setupTestDB points config.DBPath() at a temporary database and seeds it.
// Only handlers that are pure DB reads with data present are exercised here —
// handleThread/handleSend/handleDraft/handleSync all shell out to AppleScript
// against the real Apple Mail app and are deliberately not smoke-tested.
// handleSearch falls back to a live AppleScript search only when the DB has
// no matches, so seeding a matching row keeps that path DB-only too.
func setupTestDB(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mailctl.db")
	config.DBPathOverride = path
	t.Cleanup(func() { config.DBPathOverride = "" })

	s, err := store.New(path, false)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	msgs := []*models.Message{
		{ID: "1", Subject: "Invoice for July", From: "billing@example.com", To: []string{"me@example.com"}, Body: "...", Date: time.Now(), Mailbox: "INBOX", Account: "work", Source: "apple"},
		{ID: "2", Subject: "Newsletter", From: "news@example.com", To: []string{"me@example.com"}, Body: "...", Date: time.Now(), Read: true, Mailbox: "INBOX", Account: "work", Source: "apple"},
	}
	for _, m := range msgs {
		if err := s.UpsertMessage(ctx, m); err != nil {
			t.Fatalf("UpsertMessage: %v", err)
		}
	}
}

func callTool(t *testing.T, handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handler returned an error result: %+v", res.Content)
	}
	return res
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

func TestToolsAreRegisteredWithValidSchema(t *testing.T) {
	for _, tc := range []struct {
		name string
		tool mcp.Tool
	}{
		{"inbox", toolInbox()},
		{"search_email", toolSearch()},
		{"email_thread", toolThread()},
		{"send_email", toolSend()},
		{"draft_email", toolDraft()},
		{"sync_inbox", toolSync()},
	} {
		if tc.tool.Name != tc.name {
			t.Errorf("expected tool name %q, got %q", tc.name, tc.tool.Name)
		}
		if tc.tool.Description == "" {
			t.Errorf("tool %q has no description", tc.name)
		}
	}
}

func TestHandleInbox(t *testing.T) {
	setupTestDB(t)

	res := callTool(t, handleInbox, nil)
	text := resultText(t, res)
	if !strings.Contains(text, "Invoice for July") || !strings.Contains(text, "Newsletter") {
		t.Errorf("expected both seeded messages in output, got:\n%s", text)
	}
}

func TestHandleInboxUnreadOnly(t *testing.T) {
	setupTestDB(t)

	res := callTool(t, handleInbox, map[string]any{"unread_only": true})
	text := resultText(t, res)
	if !strings.Contains(text, "Invoice for July") {
		t.Errorf("expected unread message in output, got:\n%s", text)
	}
	if strings.Contains(text, "Newsletter") {
		t.Errorf("expected read message to be excluded, got:\n%s", text)
	}
}

func TestHandleSearchMatchesLocalDB(t *testing.T) {
	setupTestDB(t)

	res := callTool(t, handleSearch, map[string]any{"query": "Invoice"})
	text := resultText(t, res)
	if !strings.Contains(text, "Invoice for July") {
		t.Errorf("expected search match in output, got:\n%s", text)
	}
	if strings.Contains(text, "Newsletter") {
		t.Errorf("expected non-matching message to be excluded, got:\n%s", text)
	}
}

func TestHandleSearchRequiresQuery(t *testing.T) {
	setupTestDB(t)

	req := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{}}}
	res, err := handleSearch(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result when query is missing")
	}
}
