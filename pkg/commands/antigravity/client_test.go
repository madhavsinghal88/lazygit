package antigravity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommitMessagePrompt(t *testing.T) {
	prompt := commitMessagePrompt("diff --git a/foo.txt b/foo.txt\n+hello world")
	assert.Contains(t, prompt, "conventional commit format")
	assert.Contains(t, prompt, "diff --git a/foo.txt b/foo.txt\n+hello world")
}

func TestGenerateCommitMessageRejectsEmptyDiff(t *testing.T) {
	_, err := NewClient().GenerateCommitMessage("")
	assert.ErrorContains(t, err, "no staged changes")
}
