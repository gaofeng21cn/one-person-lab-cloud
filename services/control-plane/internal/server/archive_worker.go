package server

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultRetentionInterval = 24 * time.Hour

func retentionWorkerEnabled() bool {
	return retentionWorkerEnabledWith(os.Getenv)
}

func retentionWorkerEnabledWith(getenv func(string) string) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
	value := strings.TrimSpace(getenv("OPL_ARCHIVE_RETENTION_WORKER_ENABLED"))
	if value == "0" || strings.EqualFold(value, "false") || strings.EqualFold(value, "no") {
		return false
	}
	return value == "" || value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}

func retentionWorkerIntervalWith(getenv func(string) string) time.Duration {
	if getenv == nil {
		getenv = os.Getenv
	}
	raw := strings.TrimSpace(getenv("OPL_ARCHIVE_RETENTION_INTERVAL_MS"))
	if raw == "" {
		return defaultRetentionInterval
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return defaultRetentionInterval
	}
	return time.Duration(ms) * time.Millisecond
}

func (app *controlPlaneServer) startRetentionWorker(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultRetentionInterval
	}
	go func() {
		if err := app.runRetentionOnce(ctx); err != nil {
			log.Printf("retention failed: %v", err)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := app.runRetentionOnce(ctx); err != nil {
					log.Printf("retention failed: %v", err)
				}
			}
		}
	}()
}

func (app *controlPlaneServer) runRetentionOnce(ctx context.Context) error {
	_, err := app.applyRetention(ctx)
	return err
}
