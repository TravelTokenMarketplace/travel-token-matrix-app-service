// Copyright (C) 2022-2026, Travel Token Marketplace. All rights reserved.
// See the file LICENSE for licensing terms.

package cmd

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/TravelTokenMarketplace/travel-token-matrix-app-service/config"
	"github.com/TravelTokenMarketplace/travel-token-matrix-app-service/internal/app"
	"github.com/TravelTokenMarketplace/travel-token-matrix-app-service/internal/version"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	Version = "0.0.1"
)

var rootCmd = &cobra.Command{
	Use:        "travel-token-matrix-app-service",
	Short:      "starts travel token matrix app-service",
	Version:    Version,
	SuggestFor: []string{"travel-token-matrix", "matrix-app-service", "ttm-app-service", "app-service"},
	RunE:       rootFunc,
}

func rootFunc(cmd *cobra.Command, _ []string) error {
	configReaderLogger, err := zap.NewProduction()
	if err != nil {
		return fmt.Errorf("failed to create config-reader logger: %w", err)
	}

	sugaredConfigReaderLogger := configReaderLogger.Sugar()
	defer func() { _ = sugaredConfigReaderLogger.Sync() }()

	configReader, err := config.NewConfigReader(cmd.Flags(), sugaredConfigReaderLogger)
	if err != nil {
		return fmt.Errorf("failed to create config reader: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := configReader.ReadConfig()
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	_ = sugaredConfigReaderLogger.Sync()

	var zapLoggerConfig zap.Config
	if cfg.LogLevel == "debug" {
		zapLoggerConfig = zap.NewDevelopmentConfig()
		zapLoggerConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		zapLoggerConfig = zap.NewProductionConfig()
	}
	zapLoggerConfig.OutputPaths = []string{"stdout"}
	zapLoggerConfig.ErrorOutputPaths = []string{"stderr"}
	zapLogger, err := zapLoggerConfig.Build()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	logger := zapLogger.Sugar()
	defer func() { _ = logger.Sync() }()

	logger.Infof("App version: %s (git: %s)", version.AppVersion, version.AppGitCommit)
	logger.Infof("buf.build protocolbuffers version: %s (TTM %s)", version.BufBuildPBCommit, version.BufBuildPBTTMRelease)
	logger.Infof("buf.build grpc version: %s (TTM %s)", version.BufBuildGRPCCommit, version.BufBuildGRPCTTMRelease)
	logger.Infof("travel-token-messenger-contracts version: %s", version.ContractsGitCommit)

	app, err := app.NewApp(ctx, logger, cfg)
	if err != nil {
		logger.Error(err)
		return err
	}

	return app.Run(ctx)
}

func init() {
	cobra.EnablePrefixMatching = true
	rootCmd.Flags().AddFlagSet(config.Flags())
}

func Execute() error {
	return rootCmd.Execute()
}
