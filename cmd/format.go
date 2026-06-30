package cmd

import (
	"fmt"
	"strings"

	"github.com/aeon022/mailctl/internal/models"
)

func printMessages(msgs []models.Message) error {
	if isJSON() {
		outputJSON(msgs)
		return nil
	}
	for _, m := range msgs {
		readMark := "●"
		if m.Read {
			readMark = "○"
		}
		date := m.Date.Format("Mon Jan 02 15:04")
		from := m.From
		if len(from) > 30 {
			from = from[:28] + "…"
		}
		subject := m.Subject
		if len(subject) > 50 {
			subject = subject[:48] + "…"
		}
		fmt.Printf("%s  %s  %-30s  %s\n", readMark, date, from, subject)
		if m.Body != "" {
			preview := strings.ReplaceAll(m.Body, "\n", " ")
			if len(preview) > 80 {
				preview = preview[:78] + "…"
			}
			fmt.Printf("   %s\n", preview)
		}
		fmt.Println()
	}
	fmt.Printf("(%d messages)\n", len(msgs))
	return nil
}
