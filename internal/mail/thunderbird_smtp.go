package mail

import (
	"fmt"
	"net/smtp"
	"strings"
	"time"

	"github.com/aeon022/mailctl/internal/models"
	"github.com/google/uuid"
)

// buildMIMEMessage renders a Draft as a minimal RFC822 message: plain
// text/utf-8, no attachments, no MIME encoded-words for non-ASCII
// subjects — most clients render raw UTF-8 headers fine, but a strict
// RFC822 parser might not.
//
// ponytail: no multipart/attachment support. Callers must reject drafts
// with attachments before calling this — add multipart/mixed if a Linux
// account genuinely needs to send one.
func buildMIMEMessage(d *models.Draft, from string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(d.To, ", "))
	if len(d.CC) > 0 {
		fmt.Fprintf(&b, "Cc: %s\r\n", strings.Join(d.CC, ", "))
	}
	fmt.Fprintf(&b, "Subject: %s\r\n", d.Subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-Id: <%s@mailctl>\r\n", uuid.New().String())
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(d.Body)
	return []byte(b.String())
}

// envelopeRecipients is the SMTP RCPT TO list for a draft: To + CC + BCC.
// Headers don't deliver mail, the envelope does — a Cc: header without the
// address in RCPT TO shows a Cc line to everyone else while the CC'd person
// receives nothing. BCC is deliberately absent from the headers (that's what
// makes it blind) but must still be in the envelope. Duplicates are dropped
// so an address listed twice isn't delivered twice.
func envelopeRecipients(d *models.Draft) []string {
	var out []string
	seen := map[string]bool{}
	for _, group := range [][]string{d.To, d.CC, d.BCC} {
		for _, addr := range group {
			addr = strings.TrimSpace(addr)
			if addr == "" || seen[addr] {
				continue
			}
			seen[addr] = true
			out = append(out, addr)
		}
	}
	return out
}

// sendViaSMTP dials host:port and sends msg, using STARTTLS if the server
// offers it — net/smtp.SendMail negotiates that automatically. Only
// PLAIN/LOGIN auth is supported (no implicit TLS on 465, no OAuth2 — v1
// scope cut, see the plan's Global Constraints).
func sendViaSMTP(host string, port int, username, password, from string, to []string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	auth := smtp.PlainAuth("", username, password, host)
	return smtp.SendMail(addr, auth, from, to, msg)
}
