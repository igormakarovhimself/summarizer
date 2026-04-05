package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/igormakarovhimself/summarizer/internal/client/gigachat"
	"github.com/igormakarovhimself/summarizer/internal/client/salutespeech"
	"github.com/igormakarovhimself/summarizer/internal/config"
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

	logger, err := zap.NewDevelopment()
	if err != nil {
		log.Fatal("logger: ", err)
	}
	defer func() { _ = logger.Sync() }()
	sugar := logger.Sugar()

	b, err := tele.NewBot(tele.Settings{
		Token:  cfg.TelegramToken,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatal(err)
	}

	speechClient := salutespeech.NewSaluteSpeechClient(cfg.SaluteAuthKey, salutespeech.WithLogger(sugar))

	gigaClient := gigachat.NewGigaChatClient(cfg.GigaChatAuthKey, gigachat.WithLogger(sugar))

	db, err := repository.NewDB(cfg.DatabaseDSN)
	if err != nil {
		log.Fatal("db: ", err)
	}
	defer db.Close()
	sugar.Infoln("db connected")

	userRepo := repository.NewUserRepo(db)
	meetingRepo := repository.NewMeetingRepo(db)

	svc := service.NewSummarizationService(context.Background(), speechClient, gigaClient, userRepo, meetingRepo, sugar)

	h := handler.NewTelegramHandler(b, svc, sugar)

	b.Handle(tele.OnText, h.HandleText)
	b.Handle(tele.OnAudio, h.HandleAudio)
	b.Handle(tele.OnVoice, h.HandleVoice)

	idleConnsClosed := make(chan struct{})
	sigint := make(chan os.Signal, 1)
	signal.Notify(sigint, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)

	go func() {
		<-sigint
		sugar.Infoln("shutting down...")
		b.Stop()
		svc.Shutdown()
		close(idleConnsClosed)
	}()

	sugar.Infoln("bot started")
	b.Start()

	<-idleConnsClosed
	sugar.Infoln("shutdown complete")
}
