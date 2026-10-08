package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/afwilto/studies-mngr/configs"
	studieshttp "github.com/afwilto/studies-mngr/internal/studies/adapter/in/http"
	"github.com/afwilto/studies-mngr/internal/studies/adapter/out/postgres"
	"github.com/afwilto/studies-mngr/internal/studies/adapter/out/system"
	"github.com/afwilto/studies-mngr/internal/studies/app/usecase"
)

// main is the composition root: the only place that knows concrete adapter types.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := configs.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("creating database pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if cfg.MigrateOnStart {
		if err := runMigrations(cfg.DatabaseURL); err != nil {
			slog.Error("running migrations", "err", err)
			os.Exit(1)
		}
	}

	clock := system.Clock{}
	ids := system.UUIDv7Generator{}
	categories := postgres.NewCategoryRepository(pool)

	router := studieshttp.NewRouter(studieshttp.RouterDeps{
		Pinger:       pool,
		AllowOrigins: cfg.AllowOrigins,
		MaxBodyBytes: cfg.MaxBodyBytes,

		ListCategories: usecase.NewListCategoriesService(categories),
		CreateCategory: usecase.NewCreateCategoryService(categories, clock, ids),
		GetCategory:    usecase.NewGetCategoryService(categories),
		UpdateCategory: usecase.NewUpdateCategoryService(categories, clock),
		DeleteCategory: usecase.NewDeleteCategoryService(categories, clock),
	})

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("studies-mngr starting", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server terminated", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
}
