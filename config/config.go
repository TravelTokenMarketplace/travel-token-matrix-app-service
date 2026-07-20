// Copyright (C) 2022-2026, Travel Token Marketplace. All rights reserved.
// See the file LICENSE for licensing terms.

package config

// ******* Parsed config *******
//

type Config struct {
	LogLevel string         `mapstructure:"log_level"`
	Matrix   MatrixConfig   `mapstructure:"matrix"`
	DB       SQLiteDBConfig `mapstructure:"db"`
}

type SQLiteDBConfig struct {
	Common  UnparsedSQLiteDBConfig
	Service UnparsedSQLiteDBConfig
}

//
// ******* Common *******
//

type MatrixConfig struct {
	HTTPPort    uint64 `mapstructure:"http_port"`
	AccessToken string `mapstructure:"access_token"`
}

//
// ******* Unparsed config *******
//

type UnparsedConfig struct {
	LogLevel string                 `mapstructure:"log_level"`
	Matrix   MatrixConfig           `mapstructure:"matrix"`
	DB       UnparsedSQLiteDBConfig `mapstructure:"db"`
}

type UnparsedSQLiteDBConfig struct {
	DBPath string `mapstructure:"path"`
}

func (cfg *Config) unparse() *UnparsedConfig {
	return &UnparsedConfig{
		LogLevel: cfg.LogLevel,
		Matrix:   cfg.Matrix,
		DB:       cfg.DB.Common,
	}
}
