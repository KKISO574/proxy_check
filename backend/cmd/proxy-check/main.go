package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"proxycheck/backend/internal/api"
	"proxycheck/backend/internal/config"
	"proxycheck/backend/internal/miaospeed"
	"proxycheck/backend/internal/probe"
	"proxycheck/backend/internal/scheduler"
	"proxycheck/backend/internal/storage"
)

func main() {
	configPath := envDefault("PROXY_CHECK_CONFIG", "configs/config.yaml")
	settings, err := config.LoadSettings(configPath)
	if err != nil {
		log.Fatalf("load config %q: %v", configPath, err)
	}
	dbPath := envDefault("PROXY_CHECK_DB", config.SQLitePath(settings.App.DatabaseURL))
	addr := envFirst([]string{"PROXY_CHECK_ADDR", "PROXY_CHECK_GO_ADDR"}, ":8000")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("create data dir for %q: %v", dbPath, err)
	}

	repo, err := storage.OpenSQLite(dbPath)
	if err != nil {
		log.Fatalf("open sqlite %q: %v", dbPath, err)
	}
	defer repo.Close()
	if err := repo.EnsureSchema(); err != nil {
		log.Fatalf("ensure sqlite schema: %v", err)
	}

	controllerBaseURL := "http://" + settings.Mihomo.ControllerHost + ":" + itoa(settings.Mihomo.ControllerPort)
	delayClient := probe.NewMihomoClient(
		controllerBaseURL,
		os.Getenv(settings.Mihomo.SecretEnv),
		settings.Probe.TimeoutMS,
	)
	mihomoManager := probe.NewMihomoManager(settings)
	miaoSpeedManager := miaospeed.NewSidecarManager(miaoSpeedSidecarOptions(settings, os.Getenv(settings.MiaoSpeed.TokenEnv)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := miaoSpeedManager.Start(ctx); err != nil {
		log.Fatalf("start miaospeed sidecar: %v", err)
	}
	defer miaoSpeedManager.Stop()
	probeService := probe.NewService(repo, probe.Options{
		Concurrency:   settings.Probe.Concurrency,
		Probers:       probe.BuildProbers(settings, delayClient, nil, repo),
		BeforeTaskRun: mihomoManager.BeforeTaskRun,
	})
	defer mihomoManager.Stop()
	scheduler.New(
		repo,
		probeService,
		time.Duration(settings.Probe.IntervalSeconds)*time.Second,
	).Start(ctx)

	log.Printf("proxy-check Go API listening on %s, db=%s", addr, dbPath)
	if err := http.ListenAndServe(addr, api.NewServer(repo, api.Options{
		ConfigDir:         settings.Mihomo.ImportedConfigDir,
		StaticDir:         settings.App.StaticDir,
		ListenerPortStart: settings.Mihomo.ListenerPortStart,
		ListenerPortMax:   settings.Mihomo.ListenerPortMax,
		Runner:            probeService,
	})); err != nil {
		log.Fatal(err)
	}
}

func envDefault(name string, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

func envFirst(names []string, fallback string) string {
	for _, name := range names {
		value := os.Getenv(name)
		if value != "" {
			return value
		}
	}
	return fallback
}

func itoa(value int) string {
	return fmt.Sprintf("%d", value)
}

func miaoSpeedSidecarOptions(settings config.Settings, token string) miaospeed.SidecarOptions {
	return miaospeed.SidecarOptions{
		Enabled:        settings.MiaoSpeed.Enabled && settings.MiaoSpeed.ManageSidecar,
		Bin:            settings.MiaoSpeed.Bin,
		Args:           settings.MiaoSpeed.Args,
		WorkDir:        settings.MiaoSpeed.WorkDir,
		WSURL:          settings.MiaoSpeed.WSURL,
		Token:          token,
		StartTimeoutMS: settings.MiaoSpeed.StartTimeoutMS,
	}
}
