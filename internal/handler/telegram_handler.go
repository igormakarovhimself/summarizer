package handler

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/igormakarovhimself/summarizer/internal/service"
	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
)

type TelegramHandler struct {
	bot     *tele.Bot
	service service.SummarizationService
	logger  *zap.SugaredLogger
}

func NewTelegramHandler(bot *tele.Bot, svc service.SummarizationService, logger *zap.SugaredLogger) *TelegramHandler {
	return &TelegramHandler{
		bot:     bot,
		service: svc,
		logger:  logger,
	}
}

var errNotRegistered = fmt.Errorf("not registered")

func (h *TelegramHandler) checkRegistered(ctx context.Context, user *tele.User) error {
	ok, err := h.service.IsRegistered(ctx, user.ID)
	if err != nil {
		return err
	}
	if !ok {
		_, _ = h.bot.Send(user, "Register first: /start")
		return errNotRegistered
	}
	return nil
}

func (h *TelegramHandler) HandleText(ctx tele.Context) error {
	user := ctx.Sender()
	text := ctx.Text()
	h.logger.Infof("text from %d: %s", user.ID, text)

	switch {
	case text == "/start":
		if err := h.service.RegisterUser(context.Background(), user.ID, user.Username); err != nil {
			h.logger.Errorf("upsert user %d: %v", user.ID, err)
		}
		_, err := h.bot.Send(user, "hi")
		return err

	case text == "/list":
		if err := h.checkRegistered(context.Background(), user); err != nil {
			return nil
		}
		meetings, err := h.service.ListMeetings(context.Background(), user.ID)
		if err != nil {
			h.logger.Errorf("list meetings: %v", err)
			return err
		}
		if len(meetings) == 0 {
			_, err = h.bot.Send(user, "Встреч пока нет")
			return err
		}
		var sb strings.Builder
		for _, m := range meetings {
			sb.WriteString(fmt.Sprintf("#%d — %s — %s\n", m.ID, m.Title, m.CreatedAt.Format("02.01.2006")))
		}
		_, err = h.bot.Send(user, sb.String())
		return err

	case strings.HasPrefix(text, "/get"):
		if err := h.checkRegistered(context.Background(), user); err != nil {
			return nil
		}
		idStr := strings.TrimSpace(strings.TrimPrefix(text, "/get"))
		meetingID, err := strconv.Atoi(idStr)
		if err != nil {
			return nil
		}
		m, err := h.service.GetMeeting(context.Background(), user.ID, meetingID)
		if err != nil {
			h.logger.Errorf("get meeting %d: %v", meetingID, err)
			return err
		}
		if m == nil {
			_, err = h.bot.Send(user, "meeting not found")
			return err
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("#%d — %s\n\n", m.ID, m.Title))
		if m.Summary != "" {
			sb.WriteString(m.Summary + "\n\n")
		}
		sb.WriteString(truncate(m.Transcription, 3500))
		_, err = h.bot.Send(user, sb.String())
		return err

	case strings.HasPrefix(text, "/find"):
		if err := h.checkRegistered(context.Background(), user); err != nil {
			return nil
		}
		keyword := strings.TrimSpace(strings.TrimPrefix(text, "/find"))
		if keyword == "" {
			return nil
		}
		meetings, err := h.service.SearchMeetings(context.Background(), user.ID, keyword)
		if err != nil {
			h.logger.Errorf("search meetings: %v", err)
			return err
		}
		if len(meetings) == 0 {
			_, err = h.bot.Send(user, "not found")
			return err
		}
		var sb strings.Builder
		for _, m := range meetings {
			sb.WriteString(fmt.Sprintf("#%d — %s — %s\n", m.ID, m.Title, m.CreatedAt.Format("02.01.2006")))
		}
		_, err = h.bot.Send(user, sb.String())
		return err

	case strings.HasPrefix(text, "/chat"):
		if err := h.checkRegistered(context.Background(), user); err != nil {
			return nil
		}
		args := strings.TrimSpace(strings.TrimPrefix(text, "/chat"))
		if args == "" {
			return nil
		}

		var meetingID int
		var question string

		parts := strings.SplitN(args, " ", 2)
		if len(parts) == 2 {
			if id, err := strconv.Atoi(parts[0]); err == nil {
				meetingID = id
				question = parts[1]
			} else {
				question = args
			}
		} else {
			question = args
		}

		if strings.TrimSpace(question) == "" {
			return nil
		}

		answer, err := h.service.AskQuestion(context.Background(), user.ID, meetingID, question)
		if err != nil {
			h.logger.Errorf("gigachat err: %v", err)
			_, err = h.bot.Send(user, "Unable to get response")
			return err
		}

		_, err = h.bot.Send(user, answer)
		return err

	default:
		_, err := h.bot.Send(user, text)
		return err
	}
}

func (h *TelegramHandler) HandleAudio(ctx tele.Context) error {
	user := ctx.Sender()
	audio := ctx.Message().Audio
	h.logger.Infof("audio from %d: %s, %d bytes, %ds", user.ID, audio.FileName, audio.FileSize, audio.Duration)

	if err := h.checkRegistered(context.Background(), user); err != nil {
		return nil
	}

	fileData, err := h.downloadFile(audio.File)
	if err != nil {
		h.logger.Errorf("download err: %v", err)
		_, _ = h.bot.Send(user, "Unable to process audio")
		return err
	}

	h.service.SubmitAudio(user.ID, fileData, "audio/mpeg", "MP3", func(meetingID int, summary string, err error) {
		if err != nil {
			h.logger.Errorf("process audio err: %v", err)
			h.bot.Send(user, "Unable to process audio")
			return
		}
		msg := fmt.Sprintf("meeting #%d saved", meetingID)
		if summary != "" {
			msg += "\nMeeting:\n" + summary
		}
		h.bot.Send(user, msg)
	})

	_, _ = h.bot.Send(user, "Обрабатываю аудио...")
	return nil
}

func (h *TelegramHandler) HandleVoice(ctx tele.Context) error {
	user := ctx.Sender()
	voice := ctx.Message().Voice
	h.logger.Infof("voice from %d: %d bytes, %ds", user.ID, voice.FileSize, voice.Duration)

	if err := h.checkRegistered(context.Background(), user); err != nil {
		return nil
	}

	fileData, err := h.downloadFile(voice.File)
	if err != nil {
		h.logger.Errorf("download err: %v", err)
		_, _ = h.bot.Send(user, "Unable to process audio")
		return err
	}

	h.service.SubmitAudio(user.ID, fileData, "audio/ogg;codecs=opus", "OPUS", func(meetingID int, summary string, err error) {
		if err != nil {
			h.logger.Errorf("process voice err: %v", err)
			h.bot.Send(user, "Unable to process audio")
			return
		}
		msg := fmt.Sprintf("meeting #%d saved", meetingID)
		if summary != "" {
			msg += "\nMeeting:\n" + summary
		}
		h.bot.Send(user, msg)
	})

	_, _ = h.bot.Send(user, "Обрабатываю аудио...")
	return nil
}

func (h *TelegramHandler) downloadFile(file tele.File) ([]byte, error) {
	rc, err := h.bot.File(&file)
	if err != nil {
		return nil, fmt.Errorf("bot.File: %w", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return data, nil
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return s
}
