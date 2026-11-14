package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	me "github.com/noosphere/backend/internal/middleware"
)

const managedReadyPrefix = "NOOSPHERE_READY "

func serve(app *App, managed bool, input io.Reader, output io.Writer) (err error) {
	defer func() { err = errors.Join(err, app.Close()) }()
	addr := net.JoinHostPort(app.Config.Server.Host, strconv.Itoa(app.Config.Server.Port))
	if managed {
		addr = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	handler := me.Chain(
		app.Mux,
		me.RequestID,
		me.Logger,
		me.Recovery,
		me.CORS(app.Config.Server.AllowedOrigins),
		me.BearerAuth(os.Getenv("NOOSPHERE_BACKEND_TOKEN")),
	)
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Streaming chat responses can outlast a fixed write deadline.
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	if managed {
		if _, err := fmt.Fprintln(output, managedReadyPrefix+strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)); err != nil {
			return fmt.Errorf("report managed listener: %w", err)
		}
	}
	log.Println("Noosphere Backend ready:", listener.Addr())

	signals, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	parentDone := make(chan struct{})
	if managed {
		go func() {
			_, _ = io.Copy(io.Discard, input)
			close(parentDone)
		}()
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(listener) }()
	select {
	case err = <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-signals.Done():
	case <-parentDone:
	}

	if app.ModelLab != nil {
		app.ModelLab.StopAdmission()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		// Force HTTP connections closed, then let App.Close drain owned workers.
		log.Printf("HTTP shutdown: %v", err)
		return srv.Close()
	}
	return nil
}
