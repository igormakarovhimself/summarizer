package main

import (
	"log"
	"time"

	"github.com/igormakarovhimself/summarizer/internal/client/gigachat"
	"github.com/igormakarovhimself/summarizer/internal/client/salutespeech"
	"github.com/igormakarovhimself/summarizer/internal/handler"
	"github.com/igormakarovhimself/summarizer/internal/repository"
	tele "gopkg.in/telebot.v3"
)

func main() {
	b, err := tele.NewBot(tele.Settings{
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatal(err)
	}

	speechClient := salutespeech.NewSaluteSpeechClient()

	gigaClient := gigachat.NewGigaChatClient()

	db, err := repository.NewDB(dsn)
	if err != nil {
		log.Fatal("db: ", err)
	}
	defer db.Close()
	log.Println("db connected")

	userRepo := repository.NewUserRepo(db)

	h := handler.NewTelegramHandler(b, speechClient, gigaClient, userRepo)

	b.Handle(tele.OnText, h.HandleText)
	b.Handle(tele.OnAudio, h.HandleAudio)
	b.Handle(tele.OnVoice, h.HandleVoice)

	log.Println("bot started")
	b.Start()
}
