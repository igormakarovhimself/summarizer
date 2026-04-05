package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/igormakarovhimself/summarizer/internal/client/gigachat"
	"github.com/igormakarovhimself/summarizer/internal/client/salutespeech"
	"github.com/igormakarovhimself/summarizer/internal/repository"
)

type SummarizationServiceImpl struct {
	speechClient      *salutespeech.SaluteSpeechClient
	gigaClient        *gigachat.GigaChatClient
	userRepo          *repository.UserRepo
	meetingRepo       *repository.MeetingRepo
	mu                sync.RWMutex
	lastTranscription map[int64]string
}

func NewSummarizationService(
	speechClient *salutespeech.SaluteSpeechClient,
	gigaClient *gigachat.GigaChatClient,
	userRepo *repository.UserRepo,
	meetingRepo *repository.MeetingRepo,
) *SummarizationServiceImpl {
	return &SummarizationServiceImpl{
		speechClient:      speechClient,
		gigaClient:        gigaClient,
		userRepo:          userRepo,
		meetingRepo:       meetingRepo,
		lastTranscription: make(map[int64]string),
	}
}

func (s *SummarizationServiceImpl) RegisterUser(ctx context.Context, telegramID int64, username string) error {
	return s.userRepo.Upsert(ctx, telegramID, username)
}

func (s *SummarizationServiceImpl) IsRegistered(ctx context.Context, telegramID int64) (bool, error) {
	return s.userRepo.Exists(ctx, telegramID)
}

func (s *SummarizationServiceImpl) ProcessAudio(ctx context.Context, userID int64, audioData []byte, contentType, encoding string) (int, string, error) {
	result, err := s.speechClient.Transcribe(bytes.NewReader(audioData), contentType, encoding)
	if err != nil {
		return 0, "", fmt.Errorf("transcribe: %w", err)
	}

	transcription := extractTranscriptionText(result)
	log.Printf("transcription: %d chars", len(transcription))

	s.mu.Lock()
	s.lastTranscription[userID] = transcription
	s.mu.Unlock()

	summary, err := s.gigaClient.Summarize(transcription)
	if err != nil {
		log.Printf("summarize err: %v", err)
		summary = ""
	}

	title := fmt.Sprintf("meeting %s", time.Now().Format("02.01.2006 15:04"))
	meetingID, err := s.meetingRepo.Create(ctx, userID, title, transcription, summary)
	if err != nil {
		return 0, "", fmt.Errorf("save meeting: %w", err)
	}
	log.Printf("meeting saved: id=%d, user=%d", meetingID, userID)

	return meetingID, summary, nil
}

func (s *SummarizationServiceImpl) ListMeetings(ctx context.Context, userID int64) ([]repository.Meeting, error) {
	return s.meetingRepo.ListByUser(ctx, userID)
}

func (s *SummarizationServiceImpl) GetMeeting(ctx context.Context, userID int64, meetingID int) (*repository.Meeting, error) {
	m, err := s.meetingRepo.GetByID(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	if m == nil || m.UserID != userID {
		return nil, nil
	}
	return m, nil
}

func (s *SummarizationServiceImpl) SearchMeetings(ctx context.Context, userID int64, keyword string) ([]repository.Meeting, error) {
	return s.meetingRepo.SearchByKeyword(ctx, userID, keyword)
}

func (s *SummarizationServiceImpl) AskQuestion(ctx context.Context, userID int64, meetingID int, question string) (string, error) {
	var transcript string

	if meetingID > 0 {
		m, err := s.meetingRepo.GetByID(ctx, meetingID)
		if err != nil {
			log.Printf("chat get meeting %d: %v", meetingID, err)
		}
		if m != nil && m.UserID == userID {
			transcript = m.Transcription
		}
	} else {
		s.mu.RLock()
		t, ok := s.lastTranscription[userID]
		s.mu.RUnlock()
		if ok {
			transcript = t
		} else {
			meetings, err := s.meetingRepo.ListByUser(ctx, userID)
			if err != nil {
				log.Printf("chat list meetings: %v", err)
			}
			if len(meetings) > 0 {
				transcript = meetings[0].Transcription
			}
		}
	}

	if transcript != "" {
		log.Printf("chat from %d with context (%d chars): %s", userID, len(transcript), question)
		messages := []gigachat.Message{
			{Role: "system", Content: "Ты — помощник для анализа встреч. Вот транскрипция встречи:\n\n" + transcript},
			{Role: "user", Content: question},
		}
		return s.gigaClient.Chat(messages)
	}

	log.Printf("chat from %d without context: %s", userID, question)
	return s.gigaClient.Ask(question)
}

func extractTranscriptionText(raw []byte) string {
	var items []struct {
		Results []struct {
			NormalizedText string `json:"normalized_text"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		log.Printf("cant parse transcription json: %v", err)
		return string(raw)
	}

	var parts []string
	for _, item := range items {
		for _, r := range item.Results {
			if r.NormalizedText != "" {
				parts = append(parts, r.NormalizedText)
			}
		}
	}

	if len(parts) == 0 {
		return string(raw)
	}

	return strings.Join(parts, " ")
}
