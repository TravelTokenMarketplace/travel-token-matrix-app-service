// Copyright (C) 2022-2026, Travel Token Marketplace. All rights reserved.
// See the file LICENSE for licensing terms.

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/TravelTokenMarketplace/travel-token-matrix-app-service/internal/service"
	"github.com/TravelTokenMarketplace/travel-token-messenger-bot/v13/pkg/database/sqlite"
	"github.com/jmoiron/sqlx"
)

const (
	chunkedMessagesTableName      = "chunked_messages"
	chunkedMessageChunksTableName = "chunked_message_chunks"
)

var _ service.MessageChunksStorage = (*storage)(nil)

// outOfRangeChunk is one recorded index that the declared chunk count says
// cannot belong to the message, and the account that sent it.
type outOfRangeChunk struct {
	ChunkIndex uint32 `db:"chunk_index"`
	Sender     string `db:"sender"`
}

func (s *storage) AddChunkIndex(
	ctx context.Context,
	session service.Session,
	messageID string,
	chunkIndex uint32,
	sender string,
	firstSeenAt time.Time,
) (bool, error) {
	tx, err := sqlite.GetSQLXTx(session)
	if err != nil {
		s.base.Logger.Error(err)
		return false, err
	}

	// A chunk can arrive before the signed message that declares how many
	// chunks there are, so the tracking record is created by whichever chunk
	// gets here first and the count is filled in later.
	if _, err := tx.StmtxContext(ctx, s.insertChunkedMessage).ExecContext(ctx, messageID, firstSeenAt.Unix()); err != nil {
		s.base.Logger.Error(err)
		return false, upgradeError(err)
	}

	result, err := tx.StmtxContext(ctx, s.insertChunkIndex).ExecContext(ctx, messageID, chunkIndex, sender)
	if err != nil {
		s.base.Logger.Error(err)
		return false, upgradeError(err)
	}

	// The insert is OR IGNORE, so zero rows affected is precisely "this index
	// had already arrived" — the redelivery case, reported rather than counted.
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		s.base.Logger.Error(err)
		return false, upgradeError(err)
	}
	return rowsAffected == 1, nil
}

func (s *storage) SetExpectedChunksCount(
	ctx context.Context,
	session service.Session,
	messageID string,
	expectedChunksCount uint32,
) error {
	tx, err := sqlite.GetSQLXTx(session)
	if err != nil {
		s.base.Logger.Error(err)
		return err
	}

	result, err := tx.StmtxContext(ctx, s.setExpectedChunksCount).ExecContext(ctx, expectedChunksCount, messageID)
	if err != nil {
		s.base.Logger.Error(err)
		return upgradeError(err)
	}
	if rowsAffected, err := result.RowsAffected(); err != nil {
		s.base.Logger.Error(err)
		return upgradeError(err)
	} else if rowsAffected != 1 {
		return fmt.Errorf("error while setting expected chunks count: expected to affect 1 row, but affected %d", rowsAffected)
	}
	return nil
}

func (s *storage) GetExpectedChunksCount(ctx context.Context, session service.Session, messageID string) (uint32, error) {
	tx, err := sqlite.GetSQLXTx(session)
	if err != nil {
		s.base.Logger.Error(err)
		return 0, err
	}

	var expectedChunksCount uint32
	if err := tx.StmtxContext(ctx, s.getExpectedChunksCount).GetContext(ctx, &expectedChunksCount, messageID); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			s.base.Logger.Error(err)
		}
		return 0, upgradeError(err)
	}
	return expectedChunksCount, nil
}

func (s *storage) CountChunkIndicesBelow(
	ctx context.Context,
	session service.Session,
	messageID string,
	limit uint32,
) (uint32, error) {
	tx, err := sqlite.GetSQLXTx(session)
	if err != nil {
		s.base.Logger.Error(err)
		return 0, err
	}

	var count uint32
	if err := tx.StmtxContext(ctx, s.countChunkIndicesBelow).GetContext(ctx, &count, messageID, limit); err != nil {
		s.base.Logger.Error(err)
		return 0, upgradeError(err)
	}
	return count, nil
}

func (s *storage) FindChunkIndexAtOrAbove(
	ctx context.Context,
	session service.Session,
	messageID string,
	limit uint32,
) (uint32, string, error) {
	tx, err := sqlite.GetSQLXTx(session)
	if err != nil {
		s.base.Logger.Error(err)
		return 0, "", err
	}

	outOfRange := &outOfRangeChunk{}
	if err := tx.StmtxContext(ctx, s.findChunkIndexAtOrAbove).GetContext(ctx, outOfRange, messageID, limit); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			s.base.Logger.Error(err)
		}
		return 0, "", upgradeError(err)
	}
	return outOfRange.ChunkIndex, outOfRange.Sender, nil
}

func (s *storage) DeleteChunkIndicesAtOrAbove(
	ctx context.Context,
	session service.Session,
	messageID string,
	limit uint32,
) error {
	tx, err := sqlite.GetSQLXTx(session)
	if err != nil {
		s.base.Logger.Error(err)
		return err
	}

	if _, err := tx.StmtxContext(ctx, s.deleteChunkIndicesAtOrAbove).ExecContext(ctx, messageID, limit); err != nil {
		s.base.Logger.Error(err)
		return upgradeError(err)
	}
	return nil
}

func (s *storage) DeleteChunkedMessage(ctx context.Context, session service.Session, messageID string) error {
	tx, err := sqlite.GetSQLXTx(session)
	if err != nil {
		s.base.Logger.Error(err)
		return err
	}

	if _, err := tx.StmtxContext(ctx, s.deleteChunkIndices).ExecContext(ctx, messageID); err != nil {
		s.base.Logger.Error(err)
		return upgradeError(err)
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

func (s *storage) DeleteStalePartialMessages(ctx context.Context, session service.Session, cutoff time.Time) (int64, error) {
	tx, err := sqlite.GetSQLXTx(session)
	if err != nil {
		s.base.Logger.Error(err)
		return 0, err
	}

	if _, err := tx.StmtxContext(ctx, s.deleteStaleChunkIndices).ExecContext(ctx, cutoff.Unix()); err != nil {
		s.base.Logger.Error(err)
		return 0, upgradeError(err)
	}

	result, err := tx.StmtxContext(ctx, s.deleteStaleChunkedMessages).ExecContext(ctx, cutoff.Unix())
	if err != nil {
		s.base.Logger.Error(err)
		return 0, upgradeError(err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		s.base.Logger.Error(err)
		return 0, upgradeError(err)
	}
	return rowsAffected, nil
}

type chunkedMessagesStatements struct {
	insertChunkedMessage        *sqlx.Stmt
	setExpectedChunksCount      *sqlx.Stmt
	getExpectedChunksCount      *sqlx.Stmt
	insertChunkIndex            *sqlx.Stmt
	countChunkIndicesBelow      *sqlx.Stmt
	findChunkIndexAtOrAbove     *sqlx.Stmt
	deleteChunkIndicesAtOrAbove *sqlx.Stmt
	deleteChunkedMessage        *sqlx.Stmt
	deleteChunkIndices          *sqlx.Stmt
	deleteStaleChunkedMessages  *sqlx.Stmt
	deleteStaleChunkIndices     *sqlx.Stmt
}

func (s *storage) prepareChunkedMessagesStmts(ctx context.Context) error {
	stmts := []struct {
		query string
		into  **sqlx.Stmt
	}{
		{
			// OR IGNORE: whichever chunk arrives first creates the record and
			// later ones leave it (and its first_seen_at) alone.
			query: fmt.Sprintf(`
				INSERT OR IGNORE INTO %s (message_id, expected_chunks_count, first_seen_at)
				VALUES (?, 0, ?)
			`, chunkedMessagesTableName),
			into: &s.insertChunkedMessage,
		},
		{
			query: fmt.Sprintf(`
				UPDATE %s SET expected_chunks_count = ? WHERE message_id = ?
			`, chunkedMessagesTableName),
			into: &s.setExpectedChunksCount,
		},
		{
			query: fmt.Sprintf(`
				SELECT expected_chunks_count FROM %s WHERE message_id = ?
			`, chunkedMessagesTableName),
			into: &s.getExpectedChunksCount,
		},
		{
			// OR IGNORE against the (message_id, chunk_index) primary key is
			// what makes a redelivered chunk a no-op instead of a count.
			query: fmt.Sprintf(`
				INSERT OR IGNORE INTO %s (message_id, chunk_index, sender) VALUES (?, ?, ?)
			`, chunkedMessageChunksTableName),
			into: &s.insertChunkIndex,
		},
		{
			query: fmt.Sprintf(`
				SELECT COUNT(*) FROM %s WHERE message_id = ? AND chunk_index < ?
			`, chunkedMessageChunksTableName),
			into: &s.countChunkIndicesBelow,
		},
		{
			// Lowest first, so the reported index is stable rather than
			// whichever row the engine happened to reach first.
			query: fmt.Sprintf(`
				SELECT chunk_index, sender FROM %s
				WHERE message_id = ? AND chunk_index >= ?
				ORDER BY chunk_index LIMIT 1
			`, chunkedMessageChunksTableName),
			into: &s.findChunkIndexAtOrAbove,
		},
		{
			query: fmt.Sprintf(`
				DELETE FROM %s WHERE message_id = ? AND chunk_index >= ?
			`, chunkedMessageChunksTableName),
			into: &s.deleteChunkIndicesAtOrAbove,
		},
		{
			query: fmt.Sprintf(`DELETE FROM %s WHERE message_id = ?`, chunkedMessagesTableName),
			into:  &s.deleteChunkedMessage,
		},
		{
			query: fmt.Sprintf(`DELETE FROM %s WHERE message_id = ?`, chunkedMessageChunksTableName),
			into:  &s.deleteChunkIndices,
		},
		{
			query: fmt.Sprintf(`DELETE FROM %s WHERE first_seen_at < ?`, chunkedMessagesTableName),
			into:  &s.deleteStaleChunkedMessages,
		},
		{
			query: fmt.Sprintf(`
				DELETE FROM %s WHERE message_id IN (
					SELECT message_id FROM %s WHERE first_seen_at < ?
				)
			`, chunkedMessageChunksTableName, chunkedMessagesTableName),
			into: &s.deleteStaleChunkIndices,
		},
	}

	for _, stmt := range stmts {
		prepared, err := s.base.DB.PreparexContext(ctx, stmt.query)
		if err != nil {
			s.base.Logger.Error(err)
			return err
		}
		*stmt.into = prepared
	}
	return nil
}
