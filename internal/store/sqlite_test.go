package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/aeon022/mailctl/internal/models"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mailctl.db")
	s, err := New(path, false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sampleMessage(id string) *models.Message {
	return &models.Message{
		ID:      id,
		Subject: "Hello " + id,
		From:    "sender@example.com",
		To:      []string{"me@example.com"},
		Body:    "body of " + id,
		Date:    time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC),
		Read:    false,
		Mailbox: "INBOX",
		Account: "work",
		Source:  "apple",
	}
}

func TestUpsertAndListMessages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.UpsertMessage(ctx, sampleMessage("1")); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}
	if err := s.UpsertMessage(ctx, sampleMessage("2")); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}

	msgs, err := s.ListMessages(ctx, Filter{})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestUpsertMessageIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	m := sampleMessage("dup")
	if err := s.UpsertMessage(ctx, m); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}
	m.Subject = "Updated subject"
	if err := s.UpsertMessage(ctx, m); err != nil {
		t.Fatalf("UpsertMessage (update): %v", err)
	}

	msgs, err := s.ListMessages(ctx, Filter{})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after upsert-update, got %d", len(msgs))
	}
	if msgs[0].Subject != "Updated subject" {
		t.Errorf("expected updated subject, got %q", msgs[0].Subject)
	}
}

func TestListMessagesFilters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	unread := sampleMessage("unread")
	read := sampleMessage("read")
	read.Read = true
	other := sampleMessage("other-account")
	other.Account = "personal"

	for _, m := range []*models.Message{unread, read, other} {
		if err := s.UpsertMessage(ctx, m); err != nil {
			t.Fatalf("UpsertMessage: %v", err)
		}
	}

	unreadOnly, err := s.ListMessages(ctx, Filter{UnreadOnly: true})
	if err != nil {
		t.Fatalf("ListMessages unread: %v", err)
	}
	if len(unreadOnly) != 2 {
		t.Fatalf("expected 2 unread messages, got %d", len(unreadOnly))
	}

	byAccount, err := s.ListMessages(ctx, Filter{Account: "personal"})
	if err != nil {
		t.Fatalf("ListMessages account: %v", err)
	}
	if len(byAccount) != 1 || byAccount[0].ID != "other-account" {
		t.Fatalf("expected 1 message for account personal, got %+v", byAccount)
	}

	bySearch, err := s.ListMessages(ctx, Filter{Query: "Hello unread"})
	if err != nil {
		t.Fatalf("ListMessages query: %v", err)
	}
	if len(bySearch) != 1 || bySearch[0].ID != "unread" {
		t.Fatalf("expected 1 message matching query, got %+v", bySearch)
	}
}

func TestMarkReadUnread(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.UpsertMessage(ctx, sampleMessage("m1")); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}
	if err := s.MarkRead(ctx, "m1"); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	msgs, _ := s.ListMessages(ctx, Filter{})
	if !msgs[0].Read {
		t.Fatal("expected message to be read after MarkRead")
	}

	if err := s.MarkUnread(ctx, "m1"); err != nil {
		t.Fatalf("MarkUnread: %v", err)
	}
	msgs, _ = s.ListMessages(ctx, Filter{})
	if msgs[0].Read {
		t.Fatal("expected message to be unread after MarkUnread")
	}
}

func TestDeleteMessage(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.UpsertMessage(ctx, sampleMessage("gone")); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}
	if err := s.DeleteMessage(ctx, "gone"); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	msgs, err := s.ListMessages(ctx, Filter{})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after delete, got %d", len(msgs))
	}
}

func TestUnreadCounts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a := sampleMessage("a")
	a.Account = "work"
	b := sampleMessage("b")
	b.Account = "work"
	c := sampleMessage("c")
	c.Account = "personal"
	c.Read = true

	for _, m := range []*models.Message{a, b, c} {
		if err := s.UpsertMessage(ctx, m); err != nil {
			t.Fatalf("UpsertMessage: %v", err)
		}
	}

	counts, err := s.UnreadCounts(ctx)
	if err != nil {
		t.Fatalf("UnreadCounts: %v", err)
	}
	if counts["work"] != 2 {
		t.Errorf("expected 2 unread for work, got %d", counts["work"])
	}
	if counts["personal"] != 0 {
		t.Errorf("expected 0 unread for personal, got %d", counts["personal"])
	}
	if counts[""] != 2 {
		t.Errorf("expected total 2, got %d", counts[""])
	}
}

func TestDeleteBySource(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	apple := sampleMessage("apple-1")
	apple.Source = "apple"
	gmail := sampleMessage("gmail-1")
	gmail.Source = "gmail"

	for _, m := range []*models.Message{apple, gmail} {
		if err := s.UpsertMessage(ctx, m); err != nil {
			t.Fatalf("UpsertMessage: %v", err)
		}
	}

	if err := s.DeleteBySource(ctx, "apple"); err != nil {
		t.Fatalf("DeleteBySource: %v", err)
	}
	msgs, err := s.ListMessages(ctx, Filter{})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ID != "gmail-1" {
		t.Fatalf("expected only gmail-1 to remain, got %+v", msgs)
	}
}
