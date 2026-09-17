package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	providerID string
	modelID    string
	httpClient *http.Client
	cmd        *exec.Cmd
}

func NewClient(baseURL, providerID, modelID string) *Client {
	return &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		providerID: providerID,
		modelID:    modelID,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func NewDefaultClient() *Client {
	return NewClient("http://localhost:4096", "", "")
}

func (c *Client) EnsureRunning() error {
	if c.cmd != nil && c.cmd.Process != nil {
		return c.TestConnection()
	}

	if err := c.TestConnection(); err == nil {
		if err := c.ensureDefaultModel(); err == nil {
			return nil
		}
	}

	c.killExistingServer()

	c.cmd = exec.Command("opencode", "serve", "--port", "4096")
	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start opencode server: %w", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := c.TestConnection(); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("timed out waiting for opencode server to start")
}

func (c *Client) killExistingServer() {
	exec.Command("sh", "-c", "lsof -ti :4096 | xargs kill 2>/dev/null").Run()
	time.Sleep(500 * time.Millisecond)
}

func (c *Client) Stop() {
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait()
		c.cmd = nil
	}
}

type Session struct {
	ID string `json:"id"`
}

type TextPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type PromptRequest struct {
	Model *ModelSpec `json:"model,omitempty"`
	Parts []TextPart `json:"parts"`
}

type ModelSpec struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

type MessageResponse struct {
	Info  MessageInfo `json:"info"`
	Parts []Part      `json:"parts"`
}

type MessageInfo struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}

type Part struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type providerResponse struct {
	Connected []string          `json:"connected"`
	Default   map[string]string `json:"default"`
}

func (c *Client) ensureDefaultModel() error {
	if c.providerID != "" && c.modelID != "" {
		return nil
	}

	resp, err := c.httpClient.Get(c.baseURL + "/provider")
	if err != nil {
		return fmt.Errorf("failed to list providers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to list providers: status %d, body: %s", resp.StatusCode, string(body))
	}

	var providers providerResponse
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		return fmt.Errorf("failed to decode providers: %w", err)
	}

	for _, providerID := range providers.Connected {
		modelID, ok := providers.Default[providerID]
		if ok && modelID != "" {
			c.providerID = providerID
			c.modelID = modelID
			return nil
		}
	}

	return fmt.Errorf("no connected OpenCode providers found; run `opencode providers login`")
}

func (c *Client) createSession() (*Session, error) {
	req, err := http.NewRequest("POST", c.baseURL+"/session", bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to create session: status %d, body: %s", resp.StatusCode, string(body))
	}

	var session Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("failed to decode session response: %w", err)
	}

	return &session, nil
}

func (c *Client) sendMessage(sessionID string, prompt string) (string, error) {
	requestBody := PromptRequest{
		Model: &ModelSpec{
			ProviderID: c.providerID,
			ModelID:    c.modelID,
		},
		Parts: []TextPart{
			{Type: "text", Text: prompt},
		},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/session/%s/message", c.baseURL, sessionID)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read message response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to send message: status %d, body: %s", resp.StatusCode, string(body))
	}

	var message MessageResponse
	if err := json.Unmarshal(body, &message); err != nil {
		return "", fmt.Errorf("failed to decode message response: %w", err)
	}

	return c.extractAssistantResponse([]MessageResponse{message}), nil
}

func (c *Client) waitForIdleWithPolling(sessionID string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for response")
		case <-ticker.C:
			messages, err := c.getMessages(sessionID)
			if err != nil {
				continue
			}

			for _, msg := range messages {
				if msg.Info.Role == "assistant" {
					for _, part := range msg.Parts {
						if part.Type == "text" && part.Text != "" {
							return nil
						}
					}
				}
			}
		}
	}
}

func (c *Client) getMessages(sessionID string) ([]MessageResponse, error) {
	url := fmt.Sprintf("%s/session/%s/message", c.baseURL, sessionID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get messages: status %d, body: %s", resp.StatusCode, string(body))
	}

	var messages []MessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return nil, fmt.Errorf("failed to decode messages: %w", err)
	}

	return messages, nil
}

func (c *Client) extractAssistantResponse(messages []MessageResponse) string {
	var result strings.Builder

	for _, msg := range messages {
		if msg.Info.Role == "assistant" {
			for _, part := range msg.Parts {
				if part.Type == "text" && part.Text != "" {
					result.WriteString(part.Text)
				}
			}
		}
	}

	return strings.TrimSpace(result.String())
}

func (c *Client) GenerateCommitMessage(stagedDiff string) (string, error) {
	if stagedDiff == "" {
		return "", fmt.Errorf("no staged changes to generate commit message for")
	}

	if err := c.EnsureRunning(); err != nil {
		return "", err
	}

	if err := c.ensureDefaultModel(); err != nil {
		return "", err
	}

	prompt := fmt.Sprintf(`Generate a concise git commit message for the following staged changes.
Follow conventional commit format (type: description).
Keep the summary line under 72 characters.
If there are multiple logical changes, you can add a brief body after a blank line.
Do not use any tools, just respond with plain text containing only the commit message:

%s`, stagedDiff)

	session, err := c.createSession()
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	response, err := c.sendMessage(session.ID, prompt)
	if err != nil {
		return "", err
	}
	if response == "" {
		if err := c.waitForIdleWithPolling(session.ID, 30*time.Second); err != nil {
			return "", fmt.Errorf("no response received from AI: %w", err)
		}

		messages, err := c.getMessages(session.ID)
		if err != nil {
			return "", fmt.Errorf("failed to get messages: %w", err)
		}

		response = c.extractAssistantResponse(messages)
		if response == "" {
			return "", fmt.Errorf("no response received from AI")
		}
	}

	return response, nil
}

func (c *Client) TestConnection() error {
	req, err := http.NewRequest("GET", c.baseURL+"/global/health", nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to OpenCode server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenCode server returned status %d", resp.StatusCode)
	}

	return nil
}
