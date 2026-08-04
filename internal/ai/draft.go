package ai

import (
	"context"
	"fmt"
	"strings"

	coreai "github.com/aeon022/missionctl-core/ai"
)

const systemPrompt = `You are helping draft a professional email reply.
Given the original email subject and body, write a natural, concise reply.
Return ONLY the reply body text — no subject line, no "Dear..." opener unless it fits the tone, no signature.
Match the register and tone of the original. Be helpful and direct.`

// Draft asks the configured AI provider (Anthropic, OpenAI, Gemini, or a
// local Ollama model — see missionctl-core/ai) to generate a reply to an
// email (blocking).
func Draft(subject, body string) (string, error) {
	info, err := coreai.Detect("MAILCTL")
	if err != nil {
		return "", err
	}
	userMsg := fmt.Sprintf("Subject: %s\n\n%s", subject, body)
	result, err := coreai.Call(context.Background(), info, systemPrompt, userMsg, nil)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(result) == "" {
		return "", fmt.Errorf("blank response from %s", info.Display)
	}
	return result, nil
}
