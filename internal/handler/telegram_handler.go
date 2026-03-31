package handler

import (
	"context"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"

	"github.com/igormakarovhimself/summarizer/internal/service"
	tele "gopkg.in/telebot.v3"
)

type TelegramHandler struct {
	bot     *tele.Bot
	service service.SummarizationService
}

func NewTelegramHandler(bot *tele.Bot, svc service.SummarizationService) *TelegramHandler {
	return &TelegramHandler{
		bot:     bot,
		service: svc,
	}
}

func (h *TelegramHandler) HandleText(ctx tele.Context) error {
	user := ctx.Sender()
	text := ctx.Text()
	log.Printf("text from %d: %s", user.ID, text)

	switch {
	case text == "/start":
		if err := h.service.RegisterUser(context.Background(), user.ID, user.Username); err != nil {
			log.Printf("upsert user %d: %v", user.ID, err)
		}
		_, err := h.bot.Send(user, "hi")
		return err

	case text == "/list":
		meetings, err := h.service.ListMeetings(context.Background(), user.ID)
		if err != nil {
			log.Printf("list meetings: %v", err)
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
		idStr := strings.TrimSpace(strings.TrimPrefix(text, "/get"))
		meetingID, err := strconv.Atoi(idStr)
		if err != nil {
			return nil
		}
		m, err := h.service.GetMeeting(context.Background(), user.ID, meetingID)
		if err != nil {
			log.Printf("get meeting %d: %v", meetingID, err)
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
		keyword := strings.TrimSpace(strings.TrimPrefix(text, "/find"))
		if keyword == "" {
			return nil
		}
		meetings, err := h.service.SearchMeetings(context.Background(), user.ID, keyword)
		if err != nil {
			log.Printf("search meetings: %v", err)
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
			log.Printf("gigachat err: %v", err)
			_, err = h.bot.Send(user, "Не удалось получить ответ")
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
	log.Printf("audio from %d: %s, %d bytes, %ds", user.ID, audio.FileName, audio.FileSize, audio.Duration)

	fileData, err := h.downloadFile(audio.File)
	if err != nil {
		log.Printf("download err: %v", err)
		_, _ = h.bot.Send(user, "Не удалось обработать аудио")
		return err
	}

	meetingID, summary, err := h.service.ProcessAudio(context.Background(), user.ID, fileData, "audio/mpeg", "MP3")
	if err != nil {
		log.Printf("process audio err: %v", err)
		_, _ = h.bot.Send(user, "Не удалось обработать аудио")
		return err
	}

	msg := fmt.Sprintf("meeting #%d saved", meetingID)
	if summary != "" {
		msg += "\neeting:\n" + summary
	}
	_, _ = h.bot.Send(user, msg)
	return nil
}

func (h *TelegramHandler) HandleVoice(ctx tele.Context) error {
	user := ctx.Sender()
	voice := ctx.Message().Voice
	log.Printf("voice from %d: %d bytes, %ds", user.ID, voice.FileSize, voice.Duration)

	fileData, err := h.downloadFile(voice.File)
	if err != nil {
		log.Printf("download err: %v", err)
		_, _ = h.bot.Send(user, "Не удалось обработать аудио")
		return err
	}

	meetingID, summary, err := h.service.ProcessAudio(context.Background(), user.ID, fileData, "audio/ogg;codecs=opus", "OPUS")
	if err != nil {
		log.Printf("process voice err: %v", err)
		_, _ = h.bot.Send(user, "Не удалось обработать аудио")
		return err
	}

	msg := fmt.Sprintf("meeting #%d saved", meetingID)
	if summary != "" {
		msg += "\neeting:\n" + summary
	}
	_, _ = h.bot.Send(user, msg)
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
