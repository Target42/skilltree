package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"skilltree/internal/config"
	"skilltree/internal/db"
	"skilltree/internal/server"
	"skilltree/internal/store"
)

type App struct {
	cfg    config.Config
	log    *slog.Logger
	http   *http.Server
	cancel context.CancelFunc
}

func New(cfg config.Config, log *slog.Logger) *App {
	return &App{cfg: cfg, log: log}
}

func (a *App) Start(ctx context.Context) error {
	ctx, a.cancel = context.WithCancel(ctx)

	sqlDB, err := db.Open(ctx, a.cfg.DatabaseURL)
	if err != nil {
		return err
	}
	if err := db.Migrate(sqlDB); err != nil {
		_ = sqlDB.Close()
		return err
	}

	st := store.New(sqlDB)
	srv, err := server.New(st, a.cfg, a.log)
	if err != nil {
		_ = sqlDB.Close()
		return err
	}

	a.http = &http.Server{
		Addr:              a.cfg.HTTPAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = a.http.Shutdown(shutdownCtx)
		_ = sqlDB.Close()
	}()

	a.log.Info("listening", "addr", a.cfg.HTTPAddr)
	if a.cfg.UsingDevSessionSecret() {
		a.log.Warn("using default SKILLTREE_SESSION_SECRET; set a strong secret before production (SKILLTREE_ENV=production enforces this)")
	}
	err = a.http.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (a *App) Stop() {
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *App) Run() error {
	if err := a.cfg.Validate(); err != nil {
		return err
	}
	ctx := context.Background()
	if err := a.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	return nil
}
