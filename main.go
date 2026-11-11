package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	router "gitchat/src"
	"gitchat/src/cards"
	"gitchat/src/config"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return err
	}
	defer listener.Close()
	readyFile := os.Getenv("GITCHAT_READY_FILE")
	if readyFile != "" {
		if err := writeReadyFile(readyFile, listener.Addr()); err != nil {
			return err
		}
		defer os.Remove(readyFile)
	}
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// The native launcher owns this pipe. Closing it works on Windows as well as
	// Unix and also tears down the sidecar after an unexpected launcher exit.
	var parentClosed <-chan struct{}
	if os.Getenv("GITCHAT_EXIT_ON_STDIN_CLOSE") == "1" {
		done := make(chan struct{})
		parentClosed = done
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			close(done)
		}()
	}
	failed := make(chan error, 1)
	go func() {
		log.Printf("GitChat listening on %s (mode=%s)", listener.Addr(), cfg.Mode)
		failed <- server.Serve(listener)
	}()
	select {
	case err := <-failed:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-signals.Done():
	case <-parentClosed:
	}
	closeErr := service.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutdownErr := server.Shutdown(ctx)
	if shutdownErr != nil {
		_ = server.Close()
	}
	return errors.Join(closeErr, shutdownErr)
}

// writeReadyFile publishes only an already-bound loopback address. The launcher
// chooses a unique path in its private data directory, never the installation
// directory. Atomic rename prevents readers observing partial JSON.
func writeReadyFile(path string, address net.Addr) error {
	if !filepath.IsAbs(path) {
		return errors.New("GITCHAT_READY_FILE must be an absolute path")
	}
	tcp, ok := address.(*net.TCPAddr)
	if !ok || !tcp.IP.IsLoopback() {
		return errors.New("desktop readiness requires a loopback listening address")
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("GITCHAT_READY_FILE must not be a symbolic link or directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".gitchat-ready-*.tmp")
	if err != nil {
		return fmt.Errorf("create readiness file: %w", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err = file.Chmod(0600); err == nil {
		err = json.NewEncoder(file).Encode(struct {
			URL string `json:"url"`
			PID int    `json:"pid"`
		}{URL: "http://" + address.String(), PID: os.Getpid()})
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write readiness file: %w", err)
	}
	if err = os.Rename(temporary, path); err != nil {
		return fmt.Errorf("publish readiness file: %w", err)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
