package antigravity

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// model is a free-tier Antigravity model: low-effort Gemini Flash thinking,
// which keeps commit-message generation inside the free quota.
const model = "gemini-3.7-flash-low"

// printTimeout bounds the time we wait for the Antigravity CLI to reply.
const printTimeout = 3 * time.Minute

// Client generates commit messages by running the official Antigravity CLI.
type Client struct{}

func NewClient() *Client {
	return &Client{}
}

// GenerateCommitMessage asks Antigravity to summarize the staged diff as a
// conventional commit message.
func (c *Client) GenerateCommitMessage(stagedDiff string) (string, error) {
	if stagedDiff == "" {
		return "", fmt.Errorf("no staged changes to generate commit message for")
	}

	ctx, cancel := context.WithTimeout(context.Background(), printTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "agy", "--model", model, "--print", commitMessagePrompt(stagedDiff))
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to generate commit message: %w", err)
	}

	message := strings.TrimSpace(string(out))
	if message == "" {
		return "", fmt.Errorf("no commit message generated")
	}

	return message, nil
}

func commitMessagePrompt(stagedDiff string) string {
	return fmt.Sprintf(`Generate a concise git commit message for the following staged changes.
Follow conventional commit format (type: description).
Keep the summary line under 72 characters.
If there are multiple logical changes, you can add a brief body after a blank line.
Do not use any tools, just respond with plain text containing only the commit message:

%s`, stagedDiff)
}
