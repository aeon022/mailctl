package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/mailctl/internal/mail"
	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/mailctl/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func Serve() error {
	s := server.NewMCPServer("mailctl", "0.1.0",
		server.WithToolCapabilities(true),
	)
	s.AddTool(toolInbox(), handleInbox)
	s.AddTool(toolSearch(), handleSearch)
	s.AddTool(toolThread(), handleThread)
	s.AddTool(toolSend(), handleSend)
	s.AddTool(toolDraft(), handleDraft)
	s.AddTool(toolSync(), handleSync)
	return server.ServeStdio(s)
}

// ── Tool definitions ──────────────────────────────────────────────────────────

func toolInbox() mcp.Tool {
	return mcp.NewTool("inbox",
		mcp.WithDescription("List recent inbox messages. Returns sender, subject, date, read status, and a body preview. Good for morning briefings or checking what needs a reply."),
		mcp.WithNumber("count", mcp.Description("Max messages to return (default 20)")),
		mcp.WithBoolean("unread_only", mcp.Description("Only return unread messages")),
	)
}

func toolSearch() mcp.Tool {
	return mcp.NewTool("search_email",
		mcp.WithDescription("Search emails by keyword across subject, sender, and body."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search term")),
		mcp.WithNumber("count", mcp.Description("Max results (default 20)")),
	)
}

func toolThread() mcp.Tool {
	return mcp.NewTool("email_thread",
		mcp.WithDescription("Retrieve all messages in a thread, matched by subject. Use this to get full context before drafting a reply."),
		mcp.WithString("subject", mcp.Required(), mcp.Description("Subject (or partial subject) to search for")),
	)
}

func toolSend() mcp.Tool {
	return mcp.NewTool("send_email",
		mcp.WithDescription("Send an email via Apple Mail. Provide the full email content — the user will be asked to confirm before sending."),
		mcp.WithString("to", mcp.Required(), mcp.Description("Recipient email address (or comma-separated list)")),
		mcp.WithString("subject", mcp.Required(), mcp.Description("Email subject")),
		mcp.WithString("body", mcp.Required(), mcp.Description("Email body (plain text)")),
		mcp.WithString("cc", mcp.Description("CC recipients (comma-separated)")),
		mcp.WithString("account", mcp.Description("Sender account name (leave empty for default)")),
	)
}

func toolDraft() mcp.Tool {
	return mcp.NewTool("draft_email",
		mcp.WithDescription("Save an email to Apple Mail Drafts for review before sending. Use this when the user wants to review before sending."),
		mcp.WithString("to", mcp.Required(), mcp.Description("Recipient email address")),
		mcp.WithString("subject", mcp.Required(), mcp.Description("Email subject")),
		mcp.WithString("body", mcp.Required(), mcp.Description("Email body")),
		mcp.WithString("cc", mcp.Description("CC recipients (comma-separated)")),
		mcp.WithString("account", mcp.Description("Sender account name")),
	)
}

func toolSync() mcp.Tool {
	return mcp.NewTool("sync_inbox",
		mcp.WithDescription("Sync the inbox from Apple Mail into the local cache. Call this if the inbox data seems stale."),
		mcp.WithNumber("count", mcp.Description("Messages to sync per account (default 100)")),
	)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func handleInbox(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	count := int(req.GetFloat("count", 20))
	unreadOnly := req.GetBool("unread_only", false)

	s, err := store.New(config.DBPath())
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer s.Close()

	msgs, err := s.ListMessages(context.Background(), store.Filter{
		Mailbox:    "INBOX",
		UnreadOnly: unreadOnly,
		Limit:      count,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(msgs) == 0 {
		return mcp.NewToolResultText("Inbox is empty or not synced. Run sync_inbox first."), nil
	}
	return mcp.NewToolResultText(formatMessages(msgs, "Inbox")), nil
}

func handleSearch(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := req.GetString("query", "")
	count := int(req.GetFloat("count", 20))
	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	s, err := store.New(config.DBPath())
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer s.Close()
	msgs, err := s.ListMessages(context.Background(), store.Filter{Query: query, Limit: count})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(msgs) == 0 {
		// fall back to live search
		msgs, err = mail.SearchMessages(query, count)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
	}
	return mcp.NewToolResultText(formatMessages(msgs, fmt.Sprintf("Search: %q", query))), nil
}

func handleThread(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	subject := req.GetString("subject", "")
	if subject == "" {
		return mcp.NewToolResultError("subject is required"), nil
	}
	msgs, err := mail.FetchThread(subject, 50)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(msgs) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No messages found for subject %q", subject)), nil
	}
	return mcp.NewToolResultText(formatMessages(msgs, fmt.Sprintf("Thread: %q", subject))), nil
}

func handleSend(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d := draftFromRequest(req)
	if err := mail.Send(d); err != nil {
		return mcp.NewToolResultError("send failed: " + err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Sent: %q → %s", d.Subject, strings.Join(d.To, ", "))), nil
}

func handleDraft(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d := draftFromRequest(req)
	if err := mail.SaveDraft(d); err != nil {
		return mcp.NewToolResultError("save draft failed: " + err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Saved to Drafts: %q → %s", d.Subject, strings.Join(d.To, ", "))), nil
}

func handleSync(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	count := int(req.GetFloat("count", 100))
	msgs, err := mail.FetchInbox(count, false)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	s, err := store.New(config.DBPath())
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer s.Close()
	ctx := context.Background()
	_ = s.DeleteBySource(ctx, "apple")
	for i := range msgs {
		_ = s.UpsertMessage(ctx, &msgs[i])
	}
	return mcp.NewToolResultText(fmt.Sprintf("Synced %d messages", len(msgs))), nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func draftFromRequest(req mcp.CallToolRequest) *models.Draft {
	toStr := req.GetString("to", "")
	ccStr := req.GetString("cc", "")
	var to, cc []string
	for _, a := range strings.Split(toStr, ",") {
		if a = strings.TrimSpace(a); a != "" {
			to = append(to, a)
		}
	}
	for _, a := range strings.Split(ccStr, ",") {
		if a = strings.TrimSpace(a); a != "" {
			cc = append(cc, a)
		}
	}
	return &models.Draft{
		To:      to,
		CC:      cc,
		Subject: req.GetString("subject", ""),
		Body:    req.GetString("body", ""),
		Account: req.GetString("account", ""),
	}
}

func formatMessages(msgs []models.Message, heading string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s (%d messages):\n\n", heading, len(msgs)))
	for _, m := range msgs {
		readMark := "UNREAD"
		if m.Read {
			readMark = "read"
		}
		b.WriteString(fmt.Sprintf("From: %s\nSubject: %s\nDate: %s [%s]\n",
			m.From, m.Subject, m.Date.Format("Mon Jan 02 15:04"), readMark))
		if m.Body != "" {
			preview := strings.ReplaceAll(m.Body, "\n", " ")
			if len(preview) > 200 {
				preview = preview[:198] + "…"
			}
			b.WriteString(fmt.Sprintf("Preview: %s\n", preview))
		}
		b.WriteString("\n")
	}
	return b.String()
}

