package gigachat

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	oauthURL = "https://ngw.devices.sberbank.ru:9443"
	apiURL   = "https://gigachat.devices.sberbank.ru"
)

type Message struct {
	Role    string `json:"role"` // system, user, assistant
	Content string `json:"content"`
}

type GigaChatClient struct {
	httpClient     *http.Client
	authKey        string
	accessToken    string
	tokenExpiresAt time.Time
}

func NewGigaChatClient(authKey string) *GigaChatClient {
	return &GigaChatClient{
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, // TODO серт сбера
				},
			},
		},
		authKey: authKey,
	}
}

func (c *GigaChatClient) refreshToken() error {
	if time.Now().Before(c.tokenExpiresAt.Add(-1 * time.Minute)) {
		return nil
	}

	log.Println("gigachat: refreshing token")

	req, err := http.NewRequest("POST", oauthURL+"/api/v2/oauth", strings.NewReader("scope=GIGACHAT_API_PERS"))
	if err != nil {
		return fmt.Errorf("gigachat oauth new request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Basic "+c.authKey)
	req.Header.Set("RqUID", uuid.New().String())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gigachat oauth request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gigachat oauth: status %d, body: %s", resp.StatusCode, body)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresAt   int64  `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("gigachat oauth decode: %w", err)
	}

	c.accessToken = result.AccessToken
	c.tokenExpiresAt = time.UnixMilli(result.ExpiresAt)
	log.Printf("gigachat: token ok, expires %s", time.Until(c.tokenExpiresAt).Round(time.Second))

	return nil
}

func (c *GigaChatClient) Chat(messages []Message) (string, error) {
	if err := c.refreshToken(); err != nil {
		return "", err
	}

	reqBody := map[string]interface{}{
		"model":    "GigaChat-2",
		"messages": messages,
		"stream":   false,
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("gigachat marshal: %w", err)
	}

	log.Printf("gigachat: sending %d bytes", len(reqBytes))

	req, err := http.NewRequest("POST", apiURL+"/api/v1/chat/completions", bytes.NewReader(reqBytes))
	if err != nil {
		return "", fmt.Errorf("gigachat new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gigachat request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gigachat: status %d, body: %s", resp.StatusCode, respBody)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("gigachat decode: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("gigachat: empty choices")
	}

	answer := result.Choices[0].Message.Content
	log.Printf("gigachat: got answer, %d chars", len(answer))
	return answer, nil
}

func (c *GigaChatClient) Summarize(text string) (string, error) {
	log.Printf("gigachat: summarize %d chars", len(text))
	return c.Chat([]Message{
		{
			Role:    "user",
			Content: "Вот транскрипция встречи. Дай краткую выжимку — ключевые решения, ответственные, сроки:\n\n" + text,
		},
	})
}

func (c *GigaChatClient) Ask(question string) (string, error) {
	log.Printf("gigachat: ask '%s'", truncate(question, 80))
	return c.Chat([]Message{
		{
			Role:    "user",
			Content: question,
		},
	})
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return s
}
