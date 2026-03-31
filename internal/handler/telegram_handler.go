package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/igormakarovhimself/summarizer/internal/client/gigachat"
	"github.com/igormakarovhimself/summarizer/internal/client/salutespeech"
	"github.com/igormakarovhimself/summarizer/internal/repository"
	tele "gopkg.in/telebot.v3"
)

type TelegramHandler struct {
	bot               *tele.Bot
	speechClient      *salutespeech.SaluteSpeechClient
	gigaClient        *gigachat.GigaChatClient
	userRepo          *repository.UserRepo
	meetingRepo       *repository.MeetingRepo
	lastTranscription map[int64]string // userID -> последняя транскрипция (потом заменить на БД)
}

func NewTelegramHandler(bot *tele.Bot, speechClient *salutespeech.SaluteSpeechClient, gigaClient *gigachat.GigaChatClient, userRepo *repository.UserRepo, meetingRepo *repository.MeetingRepo) *TelegramHandler {
	return &TelegramHandler{
		bot:               bot,
		speechClient:      speechClient,
		gigaClient:        gigaClient,
		userRepo:          userRepo,
		meetingRepo:       meetingRepo,
		lastTranscription: make(map[int64]string),
	}
}

func (h *TelegramHandler) HandleText(ctx tele.Context) error {
	user := ctx.Sender()
	text := ctx.Text()
	log.Printf("text from %d: %s", user.ID, text)

	switch {
	case text == "/start":
		if err := h.userRepo.Upsert(context.Background(), user.ID, user.Username); err != nil {
			log.Printf("upsert user %d: %v", user.ID, err)
		}
		_, err := h.bot.Send(user, "hi")
		return err

	case strings.HasPrefix(text, "/chat"):
		question := strings.TrimSpace(strings.TrimPrefix(text, "/chat"))
		if question == "" {
			_, err := h.bot.Send(user, "Использование: /chat <ваш вопрос>")
			return err
		}

		var answer string
		var err error
		if transcript, ok := h.lastTranscription[user.ID]; ok {
			log.Printf("chat from %d with context (%d chars): %s", user.ID, len(transcript), question)
			messages := []gigachat.Message{
				{Role: "system", Content: "Ты — помощник для анализа встреч. Вот транскрипция последней встречи:\n\n" + transcript},
				{Role: "user", Content: question},
			}
			answer, err = h.gigaClient.Chat(messages)
		} else {
			log.Printf("chat from %d without context: %s", user.ID, question)
			answer, err = h.gigaClient.Ask(question)
		}
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

	result, err := h.speechClient.Transcribe(bytes.NewReader(fileData), "audio/mpeg", "MP3")
	if err != nil {
		log.Printf("transcribe err: %v", err)
		_, _ = h.bot.Send(user, "Не удалось обработать аудио")
		return err
	}

	transcriptionText := extractTranscriptionText(result)
	log.Printf("transcription: %d chars", len(transcriptionText))

	h.lastTranscription[user.ID] = transcriptionText

	summary, err := h.gigaClient.Summarize(transcriptionText)
	if err != nil {
		log.Printf("summarize err: %v", err)
		summary = ""
	}

	title := fmt.Sprintf("meeting %s", time.Now().Format("02.01.2006 15:04"))
	meetingID, err := h.meetingRepo.Create(context.Background(), user.ID, title, transcriptionText, summary)
	if err != nil {
		log.Printf("save meeting: %v", err)
		return err
	}
	log.Printf("meeting saved: id=%d, user=%d", meetingID, user.ID)

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

	result, err := h.speechClient.Transcribe(bytes.NewReader(fileData), "audio/ogg;codecs=opus", "OPUS")
	if err != nil {
		log.Printf("transcribe err: %v", err)
		_, _ = h.bot.Send(user, "Не удалось обработать аудио")
		return err
	}

	transcriptionText := extractTranscriptionText(result)
	log.Printf("transcription: %d chars", len(transcriptionText))

	h.lastTranscription[user.ID] = transcriptionText

	summary, err := h.gigaClient.Summarize(transcriptionText)
	if err != nil {
		log.Printf("summarize err: %v", err)
		summary = ""
	}

	title := fmt.Sprintf("meeting %s", time.Now().Format("02.01.2006 15:04"))
	meetingID, err := h.meetingRepo.Create(context.Background(), user.ID, title, transcriptionText, summary)
	if err != nil {
		log.Printf("save meeting: %v", err)
		return err
	}
	log.Printf("meeting saved: id=%d, user=%d", meetingID, user.ID)

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

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return s
}
