package markdown

import (
	"strings"
	"testing"
	"time"
)

func TestParseBasicDraft(t *testing.T) {
	src := `---
to: [jan@example.com]
cc: [lisa@example.com]
subject: "October Newsletter"
---

Hi,

here's what's new this month.
`
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.To) != 1 || d.To[0] != "jan@example.com" {
		t.Errorf("unexpected to: %v", d.To)
	}
	if len(d.CC) != 1 || d.CC[0] != "lisa@example.com" {
		t.Errorf("unexpected cc: %v", d.CC)
	}
	if d.Subject != "October Newsletter" {
		t.Errorf("unexpected subject: %q", d.Subject)
	}
	if !strings.Contains(d.Body, "what's new this month") {
		t.Errorf("unexpected body: %q", d.Body)
	}
}

func TestParseTemplateVars(t *testing.T) {
	src := `---
to: [jan@example.com]
subject: Hello
vars:
  name: Jan
---

Hi {{.name}}, today is {{.date}}.
`
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Body, "Hi Jan") {
		t.Errorf("custom var not expanded: %q", d.Body)
	}
	wantDate := time.Now().Format("January 2, 2006")
	if !strings.Contains(d.Body, wantDate) {
		t.Errorf("date var not expanded, body: %q", d.Body)
	}
}

func TestParseMissingFrontmatter(t *testing.T) {
	if _, err := Parse([]byte("just a body, no frontmatter")); err == nil {
		t.Fatal("want error for missing frontmatter")
	}
}

func TestParseUnclosedFrontmatter(t *testing.T) {
	if _, err := Parse([]byte("---\nto: [a@b.c]\nsubject: x\n\nbody")); err == nil {
		t.Fatal("want error for unclosed frontmatter")
	}
}

func TestParseMissingRequiredFields(t *testing.T) {
	noTo := `---
subject: Hello
---
body`
	if _, err := Parse([]byte(noTo)); err == nil || !strings.Contains(err.Error(), "to") {
		t.Errorf("want 'to' error, got %v", err)
	}
	noSubject := `---
to: [a@b.c]
---
body`
	if _, err := Parse([]byte(noSubject)); err == nil || !strings.Contains(err.Error(), "subject") {
		t.Errorf("want 'subject' error, got %v", err)
	}
}

func TestParseTemplate_NoToRequired(t *testing.T) {
	src := `---
subject: Following up
---
Hi {{.name}}, just checking in.`
	d, err := ParseTemplate([]byte(src))
	if err != nil {
		t.Fatalf("ParseTemplate should not require 'to': %v", err)
	}
	if d.Subject != "Following up" {
		t.Errorf("unexpected subject: %q", d.Subject)
	}
}

func TestParseTemplate_StillRequiresSubject(t *testing.T) {
	src := "---\nto: []\n---\nbody"
	if _, err := ParseTemplate([]byte(src)); err == nil || !strings.Contains(err.Error(), "subject") {
		t.Errorf("want 'subject' error, got %v", err)
	}
}
