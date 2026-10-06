// Package actlog records mailctl's user actions in the suite activity log
// (missionctl-core/activity): subjects and sender display names only — never
// message text or e-mail addresses.
package actlog

import (
	netmail "net/mail"
	"strings"

	"github.com/aeon022/missionctl-core/activity"
)

// Sent logs a successfully sent message by its subject.
func Sent(subject string) {
	if strings.TrimSpace(subject) == "" {
		subject = "(no subject)"
	}
	activity.Log("mailctl", "sent", subject)
}

// Unsubscribed logs a performed unsubscribe by the sender's display name.
func Unsubscribed(from string) { activity.Log("mailctl", "unsubscribed", SenderName(from)) }

// SenderName reduces a From header to something safe to log: the display name
// if there is one, else just the domain (an organisation, not a person's
// address), else "sender".
func SenderName(from string) string {
	if a, err := netmail.ParseAddress(from); err == nil {
		if n := strings.TrimSpace(a.Name); n != "" {
			return n
		}
		if _, domain, ok := strings.Cut(a.Address, "@"); ok && domain != "" {
			return domain
		}
	}
	// "Name <addr>" that ParseAddress rejects: keep the text before '<'
	if name, _, ok := strings.Cut(from, "<"); ok && strings.TrimSpace(name) != "" {
		return strings.Trim(strings.TrimSpace(name), `"`)
	}
	return "sender"
}
