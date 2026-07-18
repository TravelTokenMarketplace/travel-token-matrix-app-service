// Copyright (C) 2022-2026, Travel Token Marketplace. All rights reserved.
// See the file LICENSE for licensing terms.

package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/TravelTokenMarketplace/travel-token-matrix-app-service/config"
	"github.com/TravelTokenMarketplace/travel-token-matrix-app-service/internal/service"
	service_storage "github.com/TravelTokenMarketplace/travel-token-matrix-app-service/internal/storage/sqlite"
	"go.uber.org/zap"

	"golang.org/x/sync/errgroup"
)

func NewApp(ctx context.Context, logger *zap.SugaredLogger, cfg *config.Config) (*App, error) {
	serviceStorage, err := service_storage.New(
		ctx,
		logger,
		cfg.DB.Service.DBPath,
	)
	if err != nil {
		logger.Errorf("Failed to create service storage: %v", err)
		return nil, err
	}

	service := service.NewService(
		logger,
		serviceStorage,
	)

	return &App{
		cfg:        cfg,
		logger:     logger,
		service:    service,
		storage:    serviceStorage,
		httpServer: newServer(logger, cfg.Matrix.AccessToken, cfg.Matrix.HTTPPort, service),
	}, nil
}

type App struct {
	logger     *zap.SugaredLogger
	cfg        *config.Config
	service    service.Service
	httpServer *server
	storage    service_storage.Storage
}

func (a *App) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx) // error here will call gCtx.cancel() and finish other Go-s

	// run

	a.safeGo(g, func() error {
		a.logger.Info("Starting HTTP server...")
		errChan := a.httpServer.Start()
		a.logger.Info("HTTP server started.")

		if err := <-errChan; err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
			a.logger.Errorf("HTTP server stopped with error: %v", err)
			return err
		}
		return nil
	})

	// stop

	a.safeGo(g, func() error {
		<-ctx.Done()
		a.logger.Debug("Stopping HTTP server...")
		// we use background context, because we want to try to shutdown HTTP server gracefully regardless
		if err := a.httpServer.Stop(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
			a.logger.Errorf("Failed to stop HTTP server: %v", err)
			return fmt.Errorf("failed to stop HTTP server: %w", err)
		}
		a.logger.Debug("HTTP server stopped.")
		return nil
	})

	a.safeGo(g, func() error {
		<-ctx.Done()
		a.logger.Debug("Closing storage...")
		if err := a.storage.Close(); err != nil {
			a.logger.Errorf("Failed to close storage: %v", err)
			return fmt.Errorf("failed to close storage: %w", err)
		}
		a.logger.Debug("Storage closed.")
		return nil
	})

	// wait
	err := g.Wait()
	if err != nil {
		a.logger.Error(err) // will log first run/stop error
	}

	return err
}

func (a *App) safeGo(g *errgroup.Group, fn func() error) {
	g.Go(func() (err error) {
		defer func() {
			if panicErr := recover(); panicErr != nil {
				err = fmt.Errorf("panic: %v", panicErr) // err will be returned
				a.logger.Errorf("recovered from panic: %v", err)
			}
		}()
		return fn()
	})
}
