// Command sre-agent is the whole backend: HTTP API, SSE, and the worker pool
// that runs investigations. One binary, one process, one Postgres.
//
// It reaches every cluster through the Devtron orchestrator, so it needs no
// kubeconfig and no network path of its own into any managed cluster.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/adk/session"
	"google.golang.org/adk/session/database"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/devtron-labs/devtron-sre-agent/internal/agents"
	"github.com/devtron-labs/devtron-sre-agent/internal/api"
	"github.com/devtron-labs/devtron-sre-agent/internal/capability"
	"github.com/devtron-labs/devtron-sre-agent/internal/config"
	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
	"github.com/devtron-labs/devtron-sre-agent/internal/knowledge"
	"github.com/devtron-labs/devtron-sre-agent/internal/platform/log"
	"github.com/devtron-labs/devtron-sre-agent/internal/platform/pg"
	"github.com/devtron-labs/devtron-sre-agent/internal/redact"
	"github.com/devtron-labs/devtron-sre-agent/internal/runs"
	"github.com/devtron-labs/devtron-sre-agent/internal/tools"
	"github.com/devtron-labs/devtron-sre-agent/internal/worker"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := log.New(cfg.Log.Level, cfg.Log.Format)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- storage -----------------------------------------------------------
	pool, err := pg.Connect(ctx, cfg.Database.URL, cfg.Database.MaxConns)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()
	if cfg.Database.MigrateOnStart {
		if err := pg.Migrate(ctx, pool); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	store := runs.NewStore(pool)
	if n, err := store.Reclaim(ctx); err != nil {
		logger.Warn("could not reclaim in-flight runs", "err", err)
	} else if n > 0 {
		logger.Info("reclaimed runs interrupted by a previous shutdown", "count", n)
	}
	runSvc := runs.NewService(store, runs.NewBroker(), logger)

	// --- devtron access ----------------------------------------------------
	dc := devtron.New(devtron.Options{
		BaseURL:          cfg.Devtron.URL,
		Token:            cfg.Devtron.Token,
		IntelligencePath: cfg.Devtron.IntelligencePath,
		Timeout:          time.Duration(cfg.Devtron.TimeoutSeconds) * time.Second,
		InsecureTLS:      cfg.Devtron.InsecureTLS,
	})
	discoverer := devtron.NewDiscoverer(dc, 15*time.Minute)

	// Settings saved from the UI outrank the environment, which is only the
	// bootstrap path. Without either, the service still starts so the
	// settings screen is reachable.
	if stored, ok, err := store.LoadSettings(ctx); err != nil {
		logger.Warn("could not read saved settings", "err", err)
	} else if ok {
		dc.Reconfigure(stored.DevtronURL, stored.Token)
		logger.Info("using Devtron settings saved from the UI", "url", stored.DevtronURL, "by", stored.UpdatedBy)
	}
	if why := cfg.ModelCredentialMissing(); why != "" {
		logger.Warn("no model credential; investigations will fail until one is set", "reason", why)
	}
	if dc.BaseURL() == "" || !dc.HasToken() {
		logger.Warn("Devtron is not configured yet; open the settings screen to connect one",
			"url", dc.BaseURL(), "tokenSet", dc.HasToken())
	}

	caps := capability.New(dc, store, logger, runs.StaleBefore)
	caps.Hydrate(ctx)
	if dc.HasToken() {
		// Measure in the background so the first page view is not blocked by
		// a sweep that may include unreachable clusters.
		caps.RefreshInBackground(ctx)
	}

	// --- knowledge ---------------------------------------------------------
	catalog, err := knowledge.Load()
	if err != nil {
		return fmt.Errorf("knowledge packs: %w", err)
	}
	skills, err := knowledge.SkillToolset(ctx)
	if err != nil {
		return fmt.Errorf("knowledge toolset: %w", err)
	}
	logger.Info("knowledge loaded",
		"components", len(catalog.All()),
		"devtron", len(catalog.OfClass(knowledge.ClassDevtron)),
		"apps", len(catalog.OfClass(knowledge.ClassApp)))

	// --- agents ------------------------------------------------------------
	sessions, err := adkSessions(cfg.Database.URL)
	if err != nil {
		return err
	}
	pipeline := &agents.Pipeline{
		Registry: tools.All(),
		Models:   agents.NewModelFactory(cfg),
		Skills:   skills,
		Sessions: sessions,
		Budget: agents.Budget{
			MaxToolCalls:   cfg.Run.MaxToolCalls,
			MaxModelTokens: cfg.Run.MaxModelTokens,
		},
	}

	// --- worker ------------------------------------------------------------
	w := &worker.Worker{
		Runs:                runSvc,
		Devtron:             dc,
		Discoverer:          discoverer,
		Knowledge:           catalog,
		Registry:            pipeline.Registry,
		Caps:                caps,
		Pipeline:            pipeline,
		Redact:              redact.Default().Func(),
		Log:                 logger,
		PublicURL:           cfg.HTTP.PublicURL,
		Concurrency:         cfg.Run.Concurrency,
		IntelligenceTimeout: time.Duration(cfg.Run.IntelligenceTimeoutSeconds) * time.Second,
		RunTimeout:          time.Duration(cfg.Run.TimeoutSeconds) * time.Second,
	}
	w.Start(ctx)
	defer w.Stop()

	// --- http --------------------------------------------------------------
	srv := &api.Server{
		Cfg: cfg, Log: logger, Devtron: dc, Discoverer: discoverer,
		Knowledge: catalog, Runs: runSvc, Worker: w, Caps: caps,
	}
	httpSrv := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: the run stream is a long-lived SSE connection and
		// a write deadline would sever it mid-investigation.
		IdleTimeout: 120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening",
			"addr", cfg.HTTP.Addr,
			"devtron", dc.BaseURL(),
			"provider", cfg.Models.Provider,
			"concurrency", cfg.Run.Concurrency)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}

// adkSessions persists ADK agent state in the same Postgres, keyed by run id,
// so a restart can resume an investigation instead of losing its reasoning.
// ADK owns these tables and migrates them itself.
func adkSessions(url string) (session.Service, error) {
	svc, err := database.NewSessionService(
		postgres.Open(url),
		&gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)},
	)
	if err != nil {
		return nil, fmt.Errorf("adk session store: %w", err)
	}
	if err := database.AutoMigrate(svc); err != nil {
		return nil, fmt.Errorf("adk session migrate: %w", err)
	}
	return svc, nil
}
