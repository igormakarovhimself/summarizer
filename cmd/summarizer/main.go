package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log"
	"os"
	"os/signal"
	"path/filepath"
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

	var speechOpts []salutespeech.Option
	var gigaOpts []gigachat.Option

	speechOpts = append(speechOpts, salutespeech.WithLogger(sugar))
	gigaOpts = append(gigaOpts, gigachat.WithLogger(sugar))

	if cfg.CertPath != "" {
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}

		files, err := filepath.Glob(filepath.Join(cfg.CertPath, "*.cer"))
		if err != nil {
			log.Fatal("glob certs: ", err)
		}

		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				log.Fatalf("read cert %s: %v", f, err)
			}
			cert, err := x509.ParseCertificate(data)
			if err != nil {
				log.Fatalf("parse cert %s: %v", f, err)
			}
			pool.AddCert(cert)
			sugar.Infof("loaded cert: %s", f)
		}

		if len(files) > 0 {
			tlsCfg := &tls.Config{RootCAs: pool}
			speechOpts = append(speechOpts, salutespeech.WithTLSConfig(tlsCfg))
			gigaOpts = append(gigaOpts, gigachat.WithTLSConfig(tlsCfg))
		}
		sugar.Infoln("custom TLS cert loaded")
	}

	speechClient := salutespeech.NewSaluteSpeechClient(cfg.SaluteAuthKey, speechOpts...)

	gigaClient := gigachat.NewGigaChatClient(cfg.GigaChatAuthKey, gigaOpts...)

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
