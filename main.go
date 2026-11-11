package main

import (
	"context"
	"errors"
	router "gitchat/src"
	"gitchat/src/cards"
	"gitchat/src/config"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func run() error {
	cfg, err := config.GetConfig()
	if err != nil {
		return err
	}
	service, err := cards.CreateCardServer(cfg)
	if err != nil {
		return err
	}
	defer service.Close()
	server := &http.Server{Addr: cfg.ListenAddr, Handler: router.SetupRouter(&service, cfg), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	failed := make(chan error, 1)
	go func() {
		log.Printf("GitChat listening on %s (mode=%s)", cfg.ListenAddr, cfg.Mode)
		failed <- server.ListenAndServe()
	}()
	select {
	case err := <-failed:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-signals.Done():
	}
	if err := service.Close(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
