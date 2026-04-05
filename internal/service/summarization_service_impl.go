package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/igormakarovhimself/summarizer/internal/client/gigachat"
	"github.com/igormakarovhimself/summarizer/internal/client/salutespeech"
	"github.com/igormakarovhimself/summarizer/internal/repository"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type SummarizationServiceImpl struct {
	speechClient      *salutespeech.SaluteSpeechClient
	gigaClient        *gigachat.GigaChatClient
	userRepo          *repository.UserRepo
	meetingRepo       *repository.MeetingRepo
	logger            *zap.SugaredLogger
	mu                sync.RWMutex
	lastTranscription map[int64]string
	group             *errgroup.Group
	groupCtx          context.Context
	cancelFunc        context.CancelFunc
}

const maxContextChars = 8000

func NewSummarizationService(
	ctx context.Context,
	speechClient *salutespeech.SaluteSpeechClient,
	gigaClient *gigachat.GigaChatClient,
	userRepo *repository.UserRepo,
	meetingRepo *repository.MeetingRepo,
	logger *zap.SugaredLogger,
) *SummarizationServiceImpl {
	ctx, cancel := context.WithCancel(ctx)
	g, gCtx := errgroup.WithContext(ctx)

	return &SummarizationServiceImpl{
		speechClient:      speechClient,
		gigaClient:        gigaClient,
		userRepo:          userRepo,
		meetingRepo:       meetingRepo,
		logger:            logger,
		lastTranscription: make(map[int64]string),
		group:             g,
		groupCtx:          gCtx,
		cancelFunc:        cancel,
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

	transcription := s.extractTranscriptionText(result)
	s.logger.Infof("transcription: %d chars", len(transcription))

	s.mu.Lock()
	s.lastTranscription[userID] = transcription
	s.mu.Unlock()

	summary, err := s.gigaClient.Summarize(transcription)
	if err != nil {
		s.logger.Errorf("summarize err: %v", err)
		summary = ""
	}

	title := fmt.Sprintf("meeting %s", time.Now().Format("02.01.2006 15:04"))
	meetingID, err := s.meetingRepo.Create(ctx, userID, title, transcription, summary)
	if err != nil {
		return 0, "", fmt.Errorf("save meeting: %w", err)
	}
	s.logger.Infof("meeting saved: id=%d, user=%d", meetingID, userID)

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
			s.logger.Errorf("chat get meeting %d: %v", meetingID, err)
		}
		if m != nil && m.UserID == userID {
			transcript = m.Transcription
		}
	} else {
		transcript = s.findRelevantContext(ctx, userID, question)
	}

	if transcript != "" {
		s.logger.Infof("chat from %d with context (%d chars): %s", userID, len(transcript), question)
		messages := []gigachat.Message{
			{Role: "system", Content: "Ты — помощник для анализа встреч. Вот транскрипция встречи:\n\n" + transcript},
			{Role: "user", Content: question},
		}
		return s.gigaClient.Chat(messages)
	}

	s.logger.Infof("chat from %d without context: %s", userID, question)
	return s.gigaClient.Ask(question)
}

func (s *SummarizationServiceImpl) SubmitAudio(userID int64, audioData []byte, contentType, encoding string, notify func(int, string, error)) {
	s.group.Go(func() error {
		meetingID, summary, err := s.ProcessAudio(s.groupCtx, userID, audioData, contentType, encoding)
		notify(meetingID, summary, err)
		return nil
	})
}

func (s *SummarizationServiceImpl) Shutdown() {
	s.logger.Infoln("shutting down audio processor...")
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
	if s.group != nil {
		if err := s.group.Wait(); err != nil {
			s.logger.Errorf("processor shutdown: %v", err)
		}
	}
	s.logger.Infoln("audio processor stopped")
}

func (s *SummarizationServiceImpl) findRelevantContext(ctx context.Context, userID int64, question string) string {
	meetings, err := s.meetingRepo.ListByUser(ctx, userID)
	if err != nil {
		s.logger.Errorf("list meetings for context: %v", err)
		return ""
	}
	if len(meetings) == 0 {
		return ""
	}

	summaries := make([]gigachat.MeetingSummary, len(meetings))
	for i, m := range meetings {
		summaries[i] = gigachat.MeetingSummary{ID: m.ID, Title: m.Title, Summary: m.Summary}
	}

	selectedIDs, err := s.gigaClient.SelectRelevantMeetings(question, summaries)
	if err != nil {
		s.logger.Errorf("select relevant meetings: %v", err)
	}

	if len(selectedIDs) > 0 {
		idSet := make(map[int]bool, len(selectedIDs))
		for _, id := range selectedIDs {
			idSet[id] = true
		}

		var relevant []repository.Meeting
		for _, m := range meetings {
			if idSet[m.ID] {
				relevant = append(relevant, m)
			}
		}

		if len(relevant) > 0 {
			s.logger.Infof("smart context: selected %d meetings: %v", len(relevant), selectedIDs)
			return s.combineTranscriptions(relevant)
		}
	}

	s.logger.Infoln("smart context: fallback to latest meeting")
	return meetings[0].Transcription
}

func (s *SummarizationServiceImpl) combineTranscriptions(meetings []repository.Meeting) string {
	var sb strings.Builder
	for _, m := range meetings {
		header := fmt.Sprintf("--- %s (id:%d) ---\n", m.Title, m.ID)
		if sb.Len()+len(header)+len(m.Transcription) > maxContextChars {
			remaining := maxContextChars - sb.Len() - len(header)
			if remaining > 100 {
				sb.WriteString(header)
				sb.WriteString(string([]rune(m.Transcription)[:remaining]))
				sb.WriteString("...\n")
			}
			break
		}
		sb.WriteString(header)
		sb.WriteString(m.Transcription)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

func (s *SummarizationServiceImpl) extractTranscriptionText(raw []byte) string {
	var items []struct {
		Results []struct {
			NormalizedText string `json:"normalized_text"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		s.logger.Errorf("cant parse transcription json: %v", err)
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
