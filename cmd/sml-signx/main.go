package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/sml/sml-signx/internal/api"
	"github.com/sml/sml-signx/internal/config"
	"github.com/sml/sml-signx/internal/store"
	"github.com/sml/sml-signx/internal/tray"
	"github.com/sml/sml-signx/internal/version"
)

const logMaxBytes = 10 << 20 // 10 MiB

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		os.Exit(1)
	}

	showVersion := flag.Bool("version", false, "print version and exit")
	host := flag.String("host", cfg.Host, "listen host (env SML_SIGNING_HOST)")
	port := flag.Int("port", cfg.Port, "listen port (env SML_SIGNING_PORT)")
	dataDir := flag.String("data-dir", cfg.DataDir, "data directory for certificate registry and logs (env SML_DATA_DIR)")
	apiKey := flag.String("api-key", cfg.APIKey, "require this key in X-API-KEY or Authorization: Bearer (env SML_API_KEY)")
	allowUnregistered := flag.Bool("allow-unregistered-pfx", !cfg.RequireRegisteredPFX, "allow signing with PFX files that were not registered (env SML_ALLOW_UNREGISTERED_PFX)")
	tsa := flag.String("tsa", cfg.TSAURL, "optional RFC 3161 timestamp authority URL (env SML_TSA_URL)")
	headless := flag.Bool("headless", false, "run without the Windows system tray")
	flag.Parse()

	if *showVersion {
		fmt.Printf("sml-signx %s (commit %s, built %s, %s/%s)\n",
			version.Version, version.Commit, version.BuildDate, runtime.GOOS, runtime.GOARCH)
		return
	}

	cfg.Host = *host
	cfg.Port = *port
	cfg.DataDir = *dataDir
	cfg.APIKey = *apiKey
	cfg.TSAURL = *tsa
	cfg.RequireRegisteredPFX = !*allowUnregistered
	cfg.Headless = *headless

	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		os.Exit(1)
	}

	useTray := runtime.GOOS == "windows" && !cfg.Headless

	logger, closeLog := setupLogger(cfg.DataDir, !useTray)
	defer closeLog()

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		logger.Error("failed to open certificate registry", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           api.New(cfg, st, logger).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("sml-signx started",
			"version", version.Version,
			"address", cfg.Addr(),
			"data_dir", cfg.DataDir,
			"require_registered_pfx", cfg.RequireRegisteredPFX,
			"api_key_required", cfg.APIKey != "",
			"tsa_enabled", cfg.TSAURL != "",
		)
		logger.Info("endpoints",
			"health", cfg.BaseURL()+"/healthz",
			"status", cfg.BaseURL()+"/op/status",
			"register", cfg.BaseURL()+"/op/register-pfx",
			"sign", cfg.BaseURL()+"/op/sign",
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	if useTray {
		trayErr := tray.Run(tray.Options{
			Tooltip:   fmt.Sprintf("SML SignX %s - %s", version.Version, cfg.Addr()),
			StatusURL: cfg.BaseURL() + "/op/status",
			DataDir:   cfg.DataDir,
			Quit:      stop,
		})
		if trayErr != nil {
			logger.Error("system tray exited", "error", trayErr)
		}
		stop()
	} else {
		select {
		case <-ctx.Done():
			logger.Info("shutdown requested")
		case err := <-errCh:
			logger.Error("http server failed", "error", err)
			stop()
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
	logger.Info("sml-signx stopped")
}

func setupLogger(dataDir string, useStdout bool) (*slog.Logger, func()) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "warn: cannot create data dir:", err)
		return slog.New(slog.NewTextHandler(os.Stderr, nil)), func() {}
	}

	logPath := filepath.Join(dataDir, "sml-signx.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warn: cannot open log file:", err)
		return slog.New(slog.NewTextHandler(os.Stderr, nil)), func() {}
	}

	if info, statErr := f.Stat(); statErr == nil && info.Size() > logMaxBytes {
		_ = f.Truncate(0)
		_, _ = f.Seek(0, io.SeekStart)
	}

	var w io.Writer = f
	if useStdout {
		w = io.MultiWriter(os.Stdout, f)
	}

	handler := slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(handler), func() { _ = f.Close() }
}
