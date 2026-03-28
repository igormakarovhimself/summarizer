package handler

import (
	"bytes"
	"fmt"
	"io"
	"log"

	"github.com/igormakarovhimself/summarizer/internal/client/salutespeech"
	tele "gopkg.in/telebot.v3"
)

type TelegramHandler struct {
	bot          *tele.Bot
	speechClient *salutespeech.SaluteSpeechClient
}

func NewTelegramHandler(bot *tele.Bot, speechClient *salutespeech.SaluteSpeechClient) *TelegramHandler {
	return &TelegramHandler{bot: bot, speechClient: speechClient}
}

func (h *TelegramHandler) HandleText(ctx tele.Context) error {
	user := ctx.Sender()
	text := ctx.Text()
	log.Printf("text from %d: %s", user.ID, text)

	_, err := h.bot.Send(user, text)
	return err
}

func (h *TelegramHandler) HandleAudio(ctx tele.Context) error {
	user := ctx.Sender()
	audio := ctx.Message().Audio
	log.Printf("audio from %d: %s, %d bytes, %ds", user.ID, audio.FileName, audio.FileSize, audio.Duration)

	h.bot.Send(user, "Audio received")

	fileData, err := h.downloadFile(audio.File)
	if err != nil {
		h.bot.Send(user, "Unable to download file: "+err.Error())
		return err
	}
	log.Printf("downloaded %d bytes from telegram", len(fileData))

	result, err := h.speechClient.Transcribe(bytes.NewReader(fileData), "audio/mpeg", "MP3")
	if err != nil {
		h.bot.Send(user, "Unable to transcribe: "+err.Error())
		return err
	}

	text := truncate(string(result), 4096)
	_, err = h.bot.Send(user, "Transcription:\n"+text)
	return err
}

func (h *TelegramHandler) HandleVoice(ctx tele.Context) error {
	user := ctx.Sender()
	voice := ctx.Message().Voice
	log.Printf("voice from %d: %d bytes, %ds", user.ID, voice.FileSize, voice.Duration)

	h.bot.Send(user, "Voice message received")

	fileData, err := h.downloadFile(voice.File)
	if err != nil {
		h.bot.Send(user, "Unable to download file: "+err.Error())
		return err
	}
	log.Printf("downloaded %d bytes from telegram", len(fileData))

	result, err := h.speechClient.Transcribe(bytes.NewReader(fileData), "audio/ogg;codecs=opus", "OPUS")
	if err != nil {
		h.bot.Send(user, "Unable to transcribe: "+err.Error())
		return err
	}

	text := truncate(string(result), 4096)
	_, err = h.bot.Send(user, "Transcription:\n"+text)
	return err
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
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}
