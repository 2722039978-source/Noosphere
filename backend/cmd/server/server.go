package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	me "github.com/noosphere/backend/internal/middleware"
)

// serve configures the HTTP server with middleware, starts it, and handles
// graceful shutdown on SIGINT/SIGTERM.
func serve(app *App) {
	addr := fmt.Sprintf("%s:%d", app.Config.Server.Host, app.Config.Server.Port)
	handler := me.Chain(
		app.Mux,
		me.RequestID,
		me.Logger,
		me.Recovery,
		me.CORS(app.Config.Server.AllowedOrigins),
		me.BearerAuth(os.Getenv("NOOSPHERE_BACKEND_TOKEN")),
	)

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Streaming chat responses may legitimately exceed a fixed write deadline.
		WriteTimeout:   0,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// Stop HTTP admission before dependencies; App.Close drains owned workers.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		if app.ModelLab != nil {
			app.ModelLab.StopAdmission()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			srv.Close()
		}
		if err := app.Close(); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Println("Noosphere Backend ready:", addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Printf("server error: %v", err)
	}
	if err := app.Close(); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
