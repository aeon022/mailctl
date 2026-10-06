package unsub

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseRawHeadersUnfoldsAndIgnoresCase(t *testing.T) {
	raw := "From: a@b.c\r\nLIST-UNSUBSCRIBE: <mailto:u@x.test?subject=bye>,\r\n <https://x.test/u?id=1>\r\nList-Unsubscribe-Post: List-Unsubscribe=One-Click\r\nSubject: hi\r\n\r\nList-Unsubscribe: <https://in-the-body.test>\r\n"
	u, p := ParseRawHeaders(raw)
	if u != "<mailto:u@x.test?subject=bye>, <https://x.test/u?id=1>" {
		t.Errorf("unsub = %q (folded line must be joined; body must be ignored)", u)
	}
	if p != "List-Unsubscribe=One-Click" {
		t.Errorf("post = %q", p)
	}
	if u2, p2 := ParseRawHeaders("Subject: x\n\nbody"); u2 != "" || p2 != "" {
		t.Errorf("no headers → empty, got %q %q", u2, p2)
	}
}

func TestParseTargetsAndMailtoParams(t *testing.T) {
	ts := Parse("<mailto:unsub@list.test?subject=Unsubscribe%20me&body=please>, <https://list.test/u/1>, <http://plain.test/u>, <ftp://nope>", "List-Unsubscribe=One-Click")
	if len(ts) != 3 {
		t.Fatalf("targets = %+v", ts)
	}
	if ts[0].Kind != Mailto || ts[0].To != "unsub@list.test" || ts[0].Subject != "Unsubscribe me" || ts[0].Body != "please" {
		t.Errorf("mailto = %+v", ts[0])
	}
	if ts[1].Kind != OneClick || ts[2].Kind != HTTPS {
		t.Errorf("https with Post header = one-click, plain http = link only: %+v %+v", ts[1], ts[2])
	}
	// without the Post header an https target is just a link
	if got := Parse("<https://list.test/u>", ""); len(got) != 1 || got[0].Kind != HTTPS {
		t.Errorf("no post header: %+v", got)
	}
	// mailto without a subject falls back to "unsubscribe"
	if got := Parse("<mailto:a@b.test>", ""); got[0].Subject != "unsubscribe" {
		t.Errorf("default subject: %+v", got)
	}
	if got := Parse("<mailto:>", ""); len(got) != 0 {
		t.Errorf("empty mailto must be dropped: %+v", got)
	}
}

func TestChoosePrecedence(t *testing.T) {
	m := Target{Kind: Mailto, To: "a@b"}
	l := Target{Kind: HTTPS, URL: "https://l"}
	o := Target{Kind: OneClick, URL: "https://o"}
	for name, c := range map[string]struct {
		in   []Target
		want Kind
	}{
		"one-click wins": {[]Target{m, l, o}, OneClick},
		"link over mail": {[]Target{m, l}, HTTPS},
		"mail last":      {[]Target{m}, Mailto},
	} {
		if got, ok := Choose(c.in); !ok || got.Kind != c.want {
			t.Errorf("%s: got %+v", name, got)
		}
	}
	if _, ok := Choose(nil); ok {
		t.Error("no targets → no choice")
	}
}

func TestOneClickPostsExactBody(t *testing.T) {
	var gotMethod, gotBody, gotCT string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotMethod, gotBody, gotCT = r.Method, string(b), r.Header.Get("Content-Type")
		w.WriteHeader(200)
	}))
	defer srv.Close()
	old := HTTPClient
	HTTPClient = srv.Client()
	t.Cleanup(func() { HTTPClient = old })

	if err := Do(context.Background(), Target{Kind: OneClick, URL: srv.URL + "/u"}); err != nil {
		t.Fatal(err)
	}
	if gotMethod != "POST" || gotBody != "List-Unsubscribe=One-Click" || gotCT != "application/x-www-form-urlencoded" {
		t.Errorf("request = %s %q %q", gotMethod, gotBody, gotCT)
	}
}

func TestOneClickErrorsAndRefusals(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	old := HTTPClient
	HTTPClient = srv.Client()
	t.Cleanup(func() { HTTPClient = old })

	if err := Do(context.Background(), Target{Kind: OneClick, URL: srv.URL}); err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("non-2xx must be an error, got %v", err)
	}
	if err := Do(context.Background(), Target{Kind: OneClick, URL: "http://insecure.test/u"}); err == nil {
		t.Error("one-click POST over plain http must be refused")
	}
	if err := Do(context.Background(), Target{Kind: Mailto}); err == nil {
		t.Error("mailto is the caller's job")
	}
}

func TestRedirectPolicy(t *testing.T) {
	// the default client: one https redirect is followed, a second is not
	hops := 0
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops++
		if hops < 3 {
			http.Redirect(w, r, srv.URL+"/next", http.StatusTemporaryRedirect) // 307 keeps POST
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	c := *HTTPClient
	c.Transport = srv.Client().Transport
	old := HTTPClient
	HTTPClient = &c
	t.Cleanup(func() { HTTPClient = old })

	err := Do(context.Background(), Target{Kind: OneClick, URL: srv.URL + "/u"})
	if err == nil || hops != 2 {
		t.Errorf("want exactly 2 requests (original + one redirect) then an error; hops=%d err=%v", hops, err)
	}
}

func TestHTTPSOpensBrowserViaHook(t *testing.T) {
	var opened string
	old := OpenURL
	OpenURL = func(u string) error { opened = u; return nil }
	t.Cleanup(func() { OpenURL = old })
	if err := Do(context.Background(), Target{Kind: HTTPS, URL: "https://l.test/u"}); err != nil || opened != "https://l.test/u" {
		t.Errorf("opened=%q err=%v", opened, err)
	}
}
