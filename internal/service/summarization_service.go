package service

import (
	"context"

	"github.com/igormakarovhimself/summarizer/internal/repository"
)

type SummarizationService interface {
	RegisterUser(ctx context.Context, telegramID int64, username string) error
	IsRegistered(ctx context.Context, telegramID int64) (bool, error)
	ProcessAudio(ctx context.Context, userID int64, audioData []byte, contentType, encoding string) (int, string, error)
	SubmitAudio(userID int64, audioData []byte, contentType, encoding string, notify func(int, string, error))
	ListMeetings(ctx context.Context, userID int64) ([]repository.Meeting, error)
	GetMeeting(ctx context.Context, userID int64, meetingID int) (*repository.Meeting, error)
	SearchMeetings(ctx context.Context, userID int64, keyword string) ([]repository.Meeting, error)
	AskQuestion(ctx context.Context, userID int64, meetingID int, question string) (string, error)
	Shutdown()
}
