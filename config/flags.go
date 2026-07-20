// Copyright (C) 2022-2026, Travel Token Marketplace. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"github.com/spf13/pflag"
)

const flagKeyConfig = "config"

func Flags() *pflag.FlagSet {
	flags := pflag.NewFlagSet("config", pflag.ExitOnError)

	flags.String(flagKeyConfig, "travel-token-matrix-app-service.yaml", "path to config file")

	// Main config flags
	flags.String("log_level", ".", "Log level.")

	// DB config flags
	flags.String("db.path", "travel-token-matrix-app-service-db", "Path to database dir.")

	// Matrix config flags
	flags.Uint64("matrix.http_port", 9090, "App-service http port.")
	flags.String("matrix.access_token", ".", "Access token that matrix will use in requests to app-service.")

	return flags
}
