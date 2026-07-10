package ai

import (
	"context"
	"fmt"
	"os"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

var ErrNoAPIKey = fmt.Errorf("ANTHROPIC_API_KEY not set — set it in your environment")

const systemPrompt = `You are helping draft a professional email reply.
Given the original email subject and body, write a natural, concise reply.
Return ONLY the reply body text — no subject line, no "Dear..." opener unless it fits the tone, no signature.
Match the register and tone of the original. Be helpful and direct.`

// Draft calls Claude to generate a reply to an email (blocking).
func Draft(subject, body string) (string, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return "", ErrNoAPIKey
	}
	c := anthropic.NewClient(option.WithAPIKey(key))
	userMsg := fmt.Sprintf("Subject: %s\n\n%s", subject, body)
	msg, err := c.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5,
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(userMsg))},
	})
	if err != nil {
		return "", fmt.Errorf("Claude API: %w", err)
	}
	if len(msg.Content) == 0 {
		return "", fmt.Errorf("empty response from Claude")
	}
	result := msg.Content[0].Text
	if strings.TrimSpace(result) == "" {
		return "", fmt.Errorf("blank response from Claude")
	}
	return result, nil
}
