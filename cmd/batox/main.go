package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/monteirohat/batox/internal/api"
	"github.com/monteirohat/batox/internal/config"
	"github.com/monteirohat/batox/internal/db"
	"github.com/monteirohat/batox/internal/logx"
	"github.com/monteirohat/batox/internal/music"
	"github.com/monteirohat/batox/internal/onscreen"
	"github.com/monteirohat/batox/internal/platform"
	"github.com/monteirohat/batox/internal/roms"
	"github.com/monteirohat/batox/internal/systems"
)

// version is set at build time: -ldflags "-X main.version=..."
var version = "dev"

func main() {
	cfgPath := flag.String("config", "config.json", "configuration file (optional)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if err := run(*cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, "batox:", err)
		os.Exit(1)
	}
}

func run(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	logger, err := logx.New(cfg.LogDir(), cfg.Platform == config.PlatformDev)
	if err != nil {
		return err
	}
	defer logger.Close()
	logger.Infof("BatoX %s starting (platform: %s)", version, cfg.Platform)

	generated, err := cfg.EnsureToken()
	if err != nil {
		logger.Errorf("%v", err)
		return err
	}
	switch {
	case cfg.DisableAuth:
		logger.Infof("WARNING: authentication is disabled (disableAuth=true)")
	case generated:
		logger.Infof("new token generated in %s", cfg.TokenFile())
	}

	store, err := db.Open(cfg.DBFile())
	if err != nil {
		logger.Errorf("%v", err)
		return err
	}
	defer store.Close()

	reg, err := systems.Load(cfg.ESSystemsFile, cfg.RomsDir)
	if err != nil {
		logger.Errorf("%v", err)
		return err
	}
	logger.Infof("%d systems loaded from: %s", len(reg.All()), reg.Source())

	plat := platform.New(cfg, logger)
	romSvc := roms.NewService(store, reg, plat, logger)

	deps := api.Deps{
		Version:  version,
		Config:   cfg,
		Log:      logger,
		DB:       store,
		Platform: plat,
		Systems:  reg,
		Roms:     romSvc,
		Music:    music.NewService(cfg.MusicDir, logger),
	}
	if cfg.Platform == config.PlatformBatocera {
		deps.Onscreen = onscreen.New(logger) // messages on top of games, through sway
	}
	srv := api.New(deps)

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No ReadTimeout/WriteTimeout: uploading large ISOs can take minutes.
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.SyncOnStart {
		go func() {
			if _, err := romSvc.Sync(); err != nil {
				logger.Errorf("initial sync: %v", err)
			}
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Infof("API listening on %s", cfg.Listen)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Errorf("HTTP server stopped: %v", err)
			return err
		}
	case <-ctx.Done():
		logger.Infof("shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			logger.Errorf("shutdown: %v", err)
		}
	}
	logger.Infof("BatoX stopped")
	return nil
}
