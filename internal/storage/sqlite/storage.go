// Copyright (C) 2022-2026, Travel Token Marketplace. All rights reserved.
// See the file LICENSE for licensing terms.

package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"

	"github.com/TravelTokenMarketplace/travel-token-matrix-app-service/internal/service"
	"github.com/TravelTokenMarketplace/travel-token-messenger-bot/v13/pkg/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file" // required by migrate
	_ "github.com/mattn/go-sqlite3"                      // sql driver, required
	"go.uber.org/zap"
)

// dbName names this database when migrations are logged. It is passed to
// golang-migrate purely as an identifier — the migration bookkeeping lives in
// the driver's own table, whose name is independent of it — so correcting it
// from the deleted cheque subsystem's name changes no stored state.
const dbName = "app_service"

//go:embed migrations/*.sql
var embedMigrations embed.FS

var (
	_ Storage                = (*storage)(nil)
	_ service.Session        = (*sqlite.SQLxTxSession)(nil)
	_ service.SessionHandler = (*storage)(nil)
)

type Storage interface {
	Close() error

	service.Storage
}

func New(ctx context.Context, logger *zap.SugaredLogger, dbPath string) (Storage, error) {
	baseDB, err := sqlite.New(logger, embedMigrations, dbPath, dbName)
	if err != nil {
		return nil, err
	}

	s := &storage{base: baseDB}

	if err := s.prepare(ctx); err != nil {
		return nil, err
	}

	return s, nil
}

type storage struct {
	base *sqlite.DB

	chunkedMessagesStatements
}

func (s *storage) Close() error {
	return s.base.Close()
}

func (s *storage) prepare(ctx context.Context) error {
	return errors.Join(
		s.prepareChunkedMessagesStmts(ctx),
	)
}

func (s *storage) NewSession(ctx context.Context) (service.Session, error) {
	return s.base.NewSession(ctx)
}

func (s *storage) Commit(session service.Session) error {
	return s.base.Commit(session)
}

func (s *storage) Abort(session service.Session) {
	s.base.Abort(session)
}

func upgradeError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrNotFound
	}
	return err
}
