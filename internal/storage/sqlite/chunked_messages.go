// Copyright (C) 2022-2026, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/TravelTokenMarketplace/travel-token-matrix-app-service/internal/service"
	"github.com/chain4travel/camino-messenger-bot/v13/pkg/database/sqlite"
	"github.com/jmoiron/sqlx"
)

const chunkedMessagesTableName = "chunked_messages"

var _ service.MessageChunksStorage = (*storage)(nil)

type chunkedMessage struct {
	MessageID           string `db:"message_id"`
	StoredChunksCount   uint32 `db:"stored_chunks_count"`
	ExpectedChunksCount uint32 `db:"expected_chunks_count"`
}

func (s *storage) GetChunksCount(ctx context.Context, session service.Session, messageID string) (uint32, uint32, error) {
	tx, err := sqlite.GetSQLXTx(session)
	if err != nil {
		s.base.Logger.Error(err)
		return 0, 0, err
	}

	chunkedMessage := &chunkedMessage{}
	if err := tx.StmtxContext(ctx, s.getChunksCount).GetContext(ctx, chunkedMessage, messageID); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			s.base.Logger.Error(err)
		}
		return 0, 0, upgradeError(err)
	}
	return chunkedMessage.StoredChunksCount, chunkedMessage.ExpectedChunksCount, nil
}

func (s *storage) AddFirstChunk(ctx context.Context, session service.Session, messageID string, expectedChunksCount, storedChunksCount uint32) error {
	tx, err := sqlite.GetSQLXTx(session)
	if err != nil {
		s.base.Logger.Error(err)
		return err
	}

	result, err := tx.NamedStmtContext(ctx, s.upsertFirstChunk).ExecContext(ctx, chunkedMessage{
		MessageID:           messageID,
		StoredChunksCount:   storedChunksCount,
		ExpectedChunksCount: expectedChunksCount,
	})
	if err != nil {
		s.base.Logger.Error(err)
		return upgradeError(err)
	}
	if rowsAffected, err := result.RowsAffected(); err != nil {
		s.base.Logger.Error(err)
		return upgradeError(err)
	} else if rowsAffected != 1 {
		return fmt.Errorf("error while adding chunked message: expected to affect 1 row, but affected %d", rowsAffected)
	}
	return nil
}

func (s *storage) UpdateChunksCount(ctx context.Context, session service.Session, messageID string, storedChunksCount uint32) error {
	tx, err := sqlite.GetSQLXTx(session)
	if err != nil {
		s.base.Logger.Error(err)
		return err
	}

	result, err := tx.NamedStmtContext(ctx, s.upsertFirstChunk).ExecContext(ctx, chunkedMessage{
		MessageID:         messageID,
		StoredChunksCount: storedChunksCount,
	})
	if err != nil {
		s.base.Logger.Error(err)
		return upgradeError(err)
	}
	if rowsAffected, err := result.RowsAffected(); err != nil {
		s.base.Logger.Error(err)
		return upgradeError(err)
	} else if rowsAffected != 1 {
		return fmt.Errorf("error while adding message chunk: expected to affect 1 row, but affected %d", rowsAffected)
	}
	return nil
}

func (s *storage) DeleteChunkedMessage(ctx context.Context, session service.Session, messageID string) error {
	tx, err := sqlite.GetSQLXTx(session)
	if err != nil {
		s.base.Logger.Error(err)
		return err
	}

	result, err := tx.StmtxContext(ctx, s.deleteChunkedMessage).ExecContext(ctx, messageID)
	if err != nil {
		s.base.Logger.Error(err)
		return upgradeError(err)
	}
	if rowsAffected, err := result.RowsAffected(); err != nil {
		s.base.Logger.Error(err)
		return upgradeError(err)
	} else if rowsAffected != 1 {
		return fmt.Errorf("error while deleting chunked message: expected to affect 1 row, but affected %d", rowsAffected)
	}
	return nil
}

type chunkedMessagesStatements struct {
	getChunksCount, deleteChunkedMessage *sqlx.Stmt
	upsertFirstChunk                     *sqlx.NamedStmt
	upsertChunksCount                    *sqlx.NamedStmt
}

func (s *storage) prepareChunkedMessagesStmts(ctx context.Context) error {
	getChunksCount, err := s.base.DB.PreparexContext(ctx, fmt.Sprintf(`
		SELECT stored_chunks_count, expected_chunks_count FROM %s
		WHERE message_id = ?
	`, chunkedMessagesTableName))
	if err != nil {
		s.base.Logger.Error(err)
		return err
	}
	s.getChunksCount = getChunksCount

	upsertFirstChunk, err := s.base.DB.PrepareNamedContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			message_id,
			stored_chunks_count,
			expected_chunks_count
		) VALUES (
			:message_id,
			:stored_chunks_count,
			:expected_chunks_count
		)
		ON CONFLICT(message_id)
		DO UPDATE SET
			stored_chunks_count = excluded.stored_chunks_count,
			expected_chunks_count = excluded.expected_chunks_count
	`, chunkedMessagesTableName))
	if err != nil {
		s.base.Logger.Error(err)
		return err
	}
	s.upsertFirstChunk = upsertFirstChunk

	upsertChunksCount, err := s.base.DB.PrepareNamedContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			message_id,
			stored_chunks_count,
			expected_chunks_count
		) VALUES (
			:message_id,
			:stored_chunks_count,
			0
		)
		ON CONFLICT(message_id)
		DO UPDATE SET
			stored_chunks_count = excluded.stored_chunks_count
	`, chunkedMessagesTableName))
	if err != nil {
		s.base.Logger.Error(err)
		return err
	}
	s.upsertChunksCount = upsertChunksCount

	deleteChunkedMessage, err := s.base.DB.PreparexContext(ctx, fmt.Sprintf(`
		DELETE FROM %s
		WHERE message_id = ?
	`, chunkedMessagesTableName))
	if err != nil {
		s.base.Logger.Error(err)
		return err
	}
	s.deleteChunkedMessage = deleteChunkedMessage

	return nil
}
