package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/floxflix/floxflix/internal/config"
	"github.com/floxflix/floxflix/internal/database"
	"github.com/floxflix/floxflix/internal/handlers"
	"github.com/floxflix/floxflix/internal/middleware"
	"github.com/floxflix/floxflix/internal/queue"
	"github.com/floxflix/floxflix/internal/web"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	db, err := database.Connect(cfg.MongoURI)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		db.Disconnect(ctx)
	}()

	database.EnsureIndexes(db)

	q := queue.New(db, cfg)
	q.Start()

	r := chi.NewRouter()

	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.ClientIPFromRemoteAddr)
	if len(cfg.CORSOrigins) > 0 {
		r.Use(middleware.CORS(cfg.CORSOrigins))
	}
	// Only compress text assets and JSON. HLS segments and posters are already
	// compressed; gzipping them burns CPU that ffmpeg wants.
	r.Use(chimw.Compress(5,
		"text/html",
		"text/css",
		"application/javascript",
		"application/json",
		"image/svg+xml",
	))

	var spa http.Handler
	if cfg.ServeUI {
		var uiFS = web.Dist()
		if cfg.UIDir != "" {
			log.Printf("Serving UI from directory %s", cfg.UIDir)
			uiFS = os.DirFS(cfg.UIDir)
		} else {
			log.Printf("Serving embedded UI")
		}
		spa = web.NewHandler(uiFS)
		handlers.RegisterRuntimeConfig(r, cfg)
	}

	handlers.RegisterFallback(r, spa)
	handlers.RegisterHealthRoutes(r, db)
	handlers.RegisterAuthRoutes(r, db, cfg)
	handlers.RegisterUserRoutes(r, db, cfg)
	handlers.RegisterViewerRoutes(r, db, cfg)
	handlers.RegisterVideoRoutes(r, db, cfg, q)
	handlers.RegisterGenresRoutes(r, db, cfg)
	handlers.RegisterCastRoutes(r, db, cfg)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 30 * time.Second,
		WriteTimeout:      300 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("FloxFlix server (version %s) starting on :%s", version, cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("Server stopped")
}
