// Copyright (c) 2024 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package storagecleaner

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/jaegerstorage"
	"github.com/jaegertracing/jaeger/storage"
)

var (
	_ extension.Extension = (*storageCleaner)(nil)
	_ extension.Dependent = (*storageCleaner)(nil)
)

const (
	Port = "9231"
	URL  = "/purge"
)

type storageCleaner struct {
	config   *Config
	server   *http.Server
	settings component.TelemetrySettings
}

func newStorageCleaner(config *Config, telemetrySettings component.TelemetrySettings) *storageCleaner {
	return &storageCleaner{
		config:   config,
		settings: telemetrySettings,
	}
}

func (c *storageCleaner) Start(ctx context.Context, host component.Host) error {
	storageFactory, err := jaegerstorage.GetStorageFactory(c.config.TraceStorage, host)
	if err != nil {
		return fmt.Errorf("cannot find storage factory '%s': %w", c.config.TraceStorage, err)
	}

	purgeStorage := func() error {
		purger, ok := storageFactory.(storage.Purger)
		if !ok {
			return fmt.Errorf("storage %s does not implement Purger interface", c.config.TraceStorage)
		}
		if err := purger.Purge(); err != nil {
			return fmt.Errorf("error purging storage: %w", err)
		}
		return nil
	}

	purgeHandler := func(w http.ResponseWriter, r *http.Request) {
		if err := purgeStorage(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Purge request processed successfully"))
	}

	r := mux.NewRouter()
	r.HandleFunc(URL, purgeHandler).Methods(http.MethodPost)
	c.server = &http.Server{
		Addr:              ":" + c.config.Port,
		Handler:           r,
		ReadHeaderTimeout: 3 * time.Second,
	}
	go func() {
		if err := c.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			err = fmt.Errorf("error starting cleaner server: %w", err)
			c.settings.ReportStatus(component.NewFatalErrorEvent(err))
		}
	}()

	return nil
}

func (c *storageCleaner) Shutdown(ctx context.Context) error {
	if c.server != nil {
		if err := c.server.Shutdown(ctx); err != nil {
			return fmt.Errorf("error shutting down cleaner server: %w", err)
		}
	}
	return nil
}

func (c *storageCleaner) Dependencies() []component.ID {
	return []component.ID{jaegerstorage.ID}
}
