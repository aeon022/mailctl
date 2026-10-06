// Package unsub finds and performs a mail's own unsubscribe mechanism as
// advertised by its List-Unsubscribe headers (RFC 2369) and the one-click
// extension (RFC 8058). It never acts on its own: callers confirm first.
package unsub

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type Kind string

const (
	OneClick Kind = "one-click" // https + List-Unsubscribe-Post: List-Unsubscribe=One-Click
	HTTPS    Kind = "link"      // plain http(s) link — opened in the browser, never POSTed
	Mailto   Kind = "mailto"    // compose a draft; never sent automatically
)

// Target is one advertised way to unsubscribe.
type Target struct {
	Kind    Kind
	URL     string // OneClick / HTTPS
	To      string // Mailto
	Subject string
	Body    string
}

// Describe is the one-line summary shown in the confirm prompt.
func (t Target) Describe() string {
	switch t.Kind {
	case OneClick:
		return "one-click unsubscribe request to " + host(t.URL)
	case HTTPS:
		return "open the unsubscribe page at " + host(t.URL) + " in your browser"
	default:
		return "draft an unsubscribe email to " + t.To + " (you send it yourself)"
	}
}

func host(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

var angle = regexp.MustCompile(`<([^<>]+)>`)

// ParseRawHeaders extracts the List-Unsubscribe and List-Unsubscribe-Post
// values from a raw header block (CRLF or LF, folded continuation lines
// allowed, names case-insensitive).
func ParseRawHeaders(raw string) (listUnsub, post string) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	var cur *string
	for _, line := range strings.Split(raw, "\n") {
		if line == "" {
			break // end of headers
		}
		if (line[0] == ' ' || line[0] == '\t') && cur != nil {
			*cur += " " + strings.TrimSpace(line)
			continue
		}
		cur = nil
		name, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "list-unsubscribe":
			listUnsub = strings.TrimSpace(val)
			cur = &listUnsub
		case "list-unsubscribe-post":
			post = strings.TrimSpace(val)
			cur = &post
		}
	}
	return strings.TrimSpace(listUnsub), strings.TrimSpace(post)
}

// Parse turns the two header values into targets, in header order. An https
// target becomes OneClick when the Post header says so; http (no TLS) targets
// are only ever offered as a browser link.
func Parse(listUnsub, post string) []Target {
	oneClick := strings.Contains(strings.ToLower(post), "list-unsubscribe=one-click")
	var out []Target
	for _, m := range angle.FindAllStringSubmatch(listUnsub, -1) {
		raw := strings.TrimSpace(m[1])
		low := strings.ToLower(raw)
		switch {
		case strings.HasPrefix(low, "https://"):
			k := HTTPS
			if oneClick {
				k = OneClick
			}
			out = append(out, Target{Kind: k, URL: raw})
		case strings.HasPrefix(low, "http://"):
			out = append(out, Target{Kind: HTTPS, URL: raw})
		case strings.HasPrefix(low, "mailto:"):
			if t, ok := parseMailto(raw); ok {
				out = append(out, t)
			}
		}
	}
	return out
}

func parseMailto(raw string) (Target, bool) {
	rest := raw[len("mailto:"):]
	addr, query, _ := strings.Cut(rest, "?")
	addr, err := url.PathUnescape(addr)
	if err != nil || strings.TrimSpace(addr) == "" {
		return Target{}, false
	}
	t := Target{Kind: Mailto, To: strings.TrimSpace(addr), Subject: "unsubscribe"}
	if q, err := url.ParseQuery(query); err == nil {
		if s := q.Get("subject"); s != "" {
			t.Subject = s
		}
		t.Body = q.Get("body")
	}
	return t, true
}

// Choose picks the best target: one-click beats a browser link beats mailto
// (a mailto needs the user to send mail, so it is the last resort).
func Choose(ts []Target) (Target, bool) {
	for _, want := range []Kind{OneClick, HTTPS, Mailto} {
		for _, t := range ts {
			if t.Kind == want {
				return t, true
			}
		}
	}
	return Target{}, false
}

// Injectable side effects, so tests never touch the network or the OS.
var (
	HTTPClient = &http.Client{
		Timeout: 15 * time.Second,
		// Follow at most ONE redirect, and only to https: a one-click
		// endpoint that bounces us around is not one we should keep trusting.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 1 || req.URL.Scheme != "https" {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	OpenURL = func(u string) error { return exec.Command("open", u).Start() }
)

// Do performs a OneClick or HTTPS target. Mailto is the caller's job (it
// needs the compose flow). OneClick POSTs exactly "List-Unsubscribe=One-Click".
func Do(ctx context.Context, t Target) error {
	switch t.Kind {
	case OneClick:
		if !strings.HasPrefix(strings.ToLower(t.URL), "https://") {
			return fmt.Errorf("refusing one-click POST to a non-https URL")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.URL, strings.NewReader("List-Unsubscribe=One-Click"))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := HTTPClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("unsubscribe request answered %s", resp.Status)
		}
		return nil
	case HTTPS:
		return OpenURL(t.URL)
	}
	return fmt.Errorf("unsub.Do: %s targets are handled by the caller", t.Kind)
}
