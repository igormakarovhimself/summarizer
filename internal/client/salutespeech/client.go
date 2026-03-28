package salutespeech

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
	oauthURL     = "https://ngw.devices.sberbank.ru:9443"
	apiURL       = "https://smartspeech.sber.ru"
	pollInterval = 3 * time.Second
	pollTimeout  = 5 * time.Minute
)

type SaluteSpeechClient struct {
	httpClient     *http.Client
	authKey        string
	accessToken    string
	tokenExpiresAt time.Time
}

type TaskStatus struct {
	Status         string
	ResponseFileID string
}

func NewSaluteSpeechClient(authKey string) *SaluteSpeechClient {
	// TODO: ДОБАВИТЬ СЕРТ
	return &SaluteSpeechClient{
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		},
		authKey: authKey,
	}
}

func (c *SaluteSpeechClient) refreshToken() error {
	if time.Now().Before(c.tokenExpiresAt.Add(-1 * time.Minute)) {
		return nil
	}

	log.Println("salute: refreshing token...")

	req, err := http.NewRequest("POST", oauthURL+"/api/v2/oauth", strings.NewReader("scope=SALUTE_SPEECH_PERS"))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Basic "+c.authKey)
	req.Header.Set("RqUID", uuid.New().String())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("oauth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("oauth: status %d, body: %s", resp.StatusCode, body)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresAt   int64  `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("oauth decode: %w", err)
	}

	c.accessToken = result.AccessToken
	c.tokenExpiresAt = time.UnixMilli(result.ExpiresAt)
	log.Printf("salute: got token, expires in %s", time.Until(c.tokenExpiresAt).Round(time.Second))

	return nil
}

func (c *SaluteSpeechClient) UploadFile(r io.Reader, contentType string) (string, error) {
	if err := c.refreshToken(); err != nil {
		return "", err
	}

	log.Printf("salute: uploading file (%s)...", contentType)

	req, err := http.NewRequest("POST", apiURL+"/rest/v1/data:upload", r)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", contentType)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload: status %d, body: %s", resp.StatusCode, body)
	}

	var result struct {
		Status int `json:"status"`
		Result struct {
			RequestFileID string `json:"request_file_id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("upload decode: %w", err)
	}

	log.Printf("salute: uploaded, file_id=%s", result.Result.RequestFileID)
	return result.Result.RequestFileID, nil
}

func (c *SaluteSpeechClient) CreateTask(fileID string, encoding string) (string, error) {
	if err := c.refreshToken(); err != nil {
		return "", err
	}

	body := map[string]interface{}{
		"options": map[string]interface{}{
			"audio_encoding": encoding,
			"language":       "ru-RU",
		},
		"request_file_id": fileID,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	log.Printf("salute: creating task for file %s, encoding=%s", fileID, encoding)

	req, err := http.NewRequest("POST", apiURL+"/rest/v1/speech:async_recognize", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("create task request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create task: status %d, body: %s", resp.StatusCode, respBody)
	}

	var result struct {
		Status int `json:"status"`
		Result struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("create task decode: %w", err)
	}

	log.Printf("salute: task created, id=%s, status=%s", result.Result.ID, result.Result.Status)
	return result.Result.ID, nil
}

func (c *SaluteSpeechClient) GetTaskStatus(taskID string) (*TaskStatus, error) {
	if err := c.refreshToken(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", apiURL+"/rest/v1/task:get?id="+taskID, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get status request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get status: status %d, body: %s", resp.StatusCode, body)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("get status read: %w", err)
	}

	log.Printf("salute: task %s raw: %s", taskID, string(respBody))

	var result struct {
		Status int `json:"status"`
		Result struct {
			Status         string `json:"status"`
			ResponseFileID string `json:"response_file_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("get status decode: %w", err)
	}

	return &TaskStatus{
		Status:         result.Result.Status,
		ResponseFileID: result.Result.ResponseFileID,
	}, nil
}

func (c *SaluteSpeechClient) DownloadResult(responseFileID string) ([]byte, error) {
	if err := c.refreshToken(); err != nil {
		return nil, err
	}

	log.Printf("salute: downloading result %s", responseFileID)

	req, err := http.NewRequest("GET", apiURL+"/rest/v1/data:download?response_file_id="+responseFileID, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download: status %d, body: %s", resp.StatusCode, body)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("download read: %w", err)
	}

	log.Printf("salute: downloaded %d bytes", len(data))
	return data, nil
}

func (c *SaluteSpeechClient) Transcribe(r io.Reader, contentType string, audioEncoding string) ([]byte, error) {
	log.Println("salute: starting transcription...")

	fileID, err := c.UploadFile(r, contentType)
	if err != nil {
		return nil, fmt.Errorf("transcribe upload: %w", err)
	}

	taskID, err := c.CreateTask(fileID, audioEncoding)
	if err != nil {
		return nil, fmt.Errorf("transcribe create task: %w", err)
	}

	deadline := time.Now().Add(pollTimeout)
	for i := 1; ; i++ {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("transcribe: timeout after %v", pollTimeout)
		}

		time.Sleep(pollInterval)

		status, err := c.GetTaskStatus(taskID)
		if err != nil {
			return nil, fmt.Errorf("transcribe poll: %w", err)
		}

		log.Printf("salute: poll #%d, status=%s", i, status.Status)

		switch status.Status {
		case "DONE":
			if status.ResponseFileID == "" {
				return nil, fmt.Errorf("transcribe: done but no response_file_id")
			}
			data, err := c.DownloadResult(status.ResponseFileID)
			if err != nil {
				return nil, fmt.Errorf("transcribe download: %w", err)
			}
			log.Printf("salute: transcription done, %d bytes", len(data))
			return data, nil

		case "ERROR":
			return nil, fmt.Errorf("transcribe: task failed")
		case "CANCELED":
			return nil, fmt.Errorf("transcribe: task canceled")
		case "NEW", "RUNNING":
			continue
		default:
			return nil, fmt.Errorf("transcribe: unknown status %q", status.Status)
		}
	}
}
