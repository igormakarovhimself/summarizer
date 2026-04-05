package main

import (
	"log"
	"time"

	"github.com/igormakarovhimself/summarizer/internal/config"
	"github.com/igormakarovhimself/summarizer/internal/client/gigachat"
	"github.com/igormakarovhimself/summarizer/internal/client/salutespeech"
	"github.com/igormakarovhimself/summarizer/internal/handler"
	"github.com/igormakarovhimself/summarizer/internal/repository"
	"github.com/igormakarovhimself/summarizer/internal/service"
	tele "gopkg.in/telebot.v3"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("config: ", err)
	}

	b, err := tele.NewBot(tele.Settings{
		Token:  cfg.TelegramToken,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatal(err)
	}

	speechClient := salutespeech.NewSaluteSpeechClient(cfg.SaluteAuthKey)

	gigaClient := gigachat.NewGigaChatClient(cfg.GigaChatAuthKey)

	db, err := repository.NewDB(cfg.DatabaseDSN)
	if err != nil {
		log.Fatal("db: ", err)
	}
	defer db.Close()
	log.Println("db connected")

	userRepo := repository.NewUserRepo(db)
	meetingRepo := repository.NewMeetingRepo(db)

	service := service.NewSummarizationService(speechClient, gigaClient, userRepo, meetingRepo)

	h := handler.NewTelegramHandler(b, service)

	b.Handle(tele.OnText, h.HandleText)
	b.Handle(tele.OnAudio, h.HandleAudio)
	b.Handle(tele.OnVoice, h.HandleVoice)

	log.Println("bot started")
	b.Start()
}
