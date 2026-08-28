// Copyright (C) 2022-2026, Travel Token Marketplace. All rights reserved.
// See the file LICENSE for licensing terms.

package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/TravelTokenMarketplace/travel-token-matrix-app-service/internal/service"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const testSender = "@bot:example.org"

// newTestStorage opens a real SQLite database in a temp dir and runs the full
// migration chain against it, so these tests also prove that the chunk-index
// migration applies on top of the original schema.
func newTestStorage(t *testing.T) Storage {
	t.Helper()
	storage, err := New(context.Background(), zap.NewNop().Sugar(), filepath.Join(t.TempDir(), "test-db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })
	return storage
}

func withSession(t *testing.T, storage Storage, fn func(session service.Session)) {
	t.Helper()
	session, err := storage.NewSession(context.Background())
	require.NoError(t, err)
	defer storage.Abort(session)
	fn(session)
	require.NoError(t, storage.Commit(session))
}

func TestMigrationsApplyCleanly(t *testing.T) {
	newTestStorage(t) // New() runs the migrations; a failure surfaces here.
}

// The property the whole schema change exists for: recording the same index
// twice must not look like two chunks.
func TestAddChunkIndexIgnoresARepeatedIndex(t *testing.T) {
	storage := newTestStorage(t)
	ctx := context.Background()
	now := time.Now()

	withSession(t, storage, func(session service.Session) {
		recorded, err := storage.AddChunkIndex(ctx, session, "m1", 1, testSender, now)
		require.NoError(t, err)
		require.True(t, recorded, "first sighting of an index is recorded")

		recorded, err = storage.AddChunkIndex(ctx, session, "m1", 1, testSender, now)
		require.NoError(t, err)
		require.False(t, recorded, "the same index again is a redelivery, not a new chunk")

		recorded, err = storage.AddChunkIndex(ctx, session, "m1", 2, testSender, now)
		require.NoError(t, err)
		require.True(t, recorded)

		count, err := storage.CountChunkIndicesBelow(ctx, session, "m1", 3)
		require.NoError(t, err)
		require.Equal(t, uint32(2), count, "two distinct indices, three arrivals")
	})
}

func TestCountChunkIndicesBelowIgnoresOutOfRangeIndices(t *testing.T) {
	storage := newTestStorage(t)
	ctx := context.Background()
	now := time.Now()

	withSession(t, storage, func(session service.Session) {
		for _, index := range []uint32{0, 1, 9} {
			_, err := storage.AddChunkIndex(ctx, session, "m1", index, testSender, now)
			require.NoError(t, err)
		}

		count, err := storage.CountChunkIndicesBelow(ctx, session, "m1", 3)
		require.NoError(t, err)
		require.Equal(t, uint32(2), count,
			"an index beyond the declared count must not help complete the message")
	})
}

func TestExpectedChunksCountIsUnknownUntilSet(t *testing.T) {
	storage := newTestStorage(t)
	ctx := context.Background()
	now := time.Now()

	withSession(t, storage, func(session service.Session) {
		_, err := storage.GetExpectedChunksCount(ctx, session, "absent")
		require.ErrorIs(t, err, service.ErrNotFound)

		_, err = storage.AddChunkIndex(ctx, session, "m1", 1, testSender, now)
		require.NoError(t, err)

		expected, err := storage.GetExpectedChunksCount(ctx, session, "m1")
		require.NoError(t, err)
		require.Zero(t, expected, "a chunk that overtakes the signed message leaves the count unknown")

		require.NoError(t, storage.SetExpectedChunksCount(ctx, session, "m1", 4))
		expected, err = storage.GetExpectedChunksCount(ctx, session, "m1")
		require.NoError(t, err)
		require.Equal(t, uint32(4), expected)
	})
}

// The first chunk to arrive fixes first_seen_at; later ones must not push it
// forward, or a steady trickle would keep a stuck message alive forever.
func TestFirstSeenIsNotRefreshedByLaterChunks(t *testing.T) {
	storage := newTestStorage(t)
	ctx := context.Background()
	start := time.Now()

	withSession(t, storage, func(session service.Session) {
		_, err := storage.AddChunkIndex(ctx, session, "m1", 0, testSender, start)
		require.NoError(t, err)
		_, err = storage.AddChunkIndex(ctx, session, "m1", 1, testSender, start.Add(10*time.Minute))
		require.NoError(t, err)
	})

	withSession(t, storage, func(session service.Session) {
		deleted, err := storage.DeleteStalePartialMessages(ctx, session, start.Add(time.Minute))
		require.NoError(t, err)
		require.Equal(t, int64(1), deleted)

		count, err := storage.CountChunkIndicesBelow(ctx, session, "m1", 10)
		require.NoError(t, err)
		require.Zero(t, count, "sweeping a message must take its chunk rows with it")
	})
}

func TestDeleteChunkedMessageRemovesItsChunks(t *testing.T) {
	storage := newTestStorage(t)
	ctx := context.Background()
	now := time.Now()

	withSession(t, storage, func(session service.Session) {
		_, err := storage.AddChunkIndex(ctx, session, "m1", 0, testSender, now)
		require.NoError(t, err)
		_, err = storage.AddChunkIndex(ctx, session, "m1", 1, testSender, now)
		require.NoError(t, err)

		require.NoError(t, storage.DeleteChunkedMessage(ctx, session, "m1"))

		count, err := storage.CountChunkIndicesBelow(ctx, session, "m1", 10)
		require.NoError(t, err)
		require.Zero(t, count)

		_, err = storage.GetExpectedChunksCount(ctx, session, "m1")
		require.ErrorIs(t, err, service.ErrNotFound)
	})
}

func TestDeleteStalePartialMessagesLeavesFreshOnesAlone(t *testing.T) {
	storage := newTestStorage(t)
	ctx := context.Background()
	now := time.Now()

	withSession(t, storage, func(session service.Session) {
		_, err := storage.AddChunkIndex(ctx, session, "old", 0, testSender, now.Add(-time.Hour))
		require.NoError(t, err)
		_, err = storage.AddChunkIndex(ctx, session, "fresh", 0, testSender, now)
		require.NoError(t, err)

		deleted, err := storage.DeleteStalePartialMessages(ctx, session, now.Add(-time.Minute))
		require.NoError(t, err)
		require.Equal(t, int64(1), deleted)

		_, err = storage.GetExpectedChunksCount(ctx, session, "old")
		require.ErrorIs(t, err, service.ErrNotFound)
		_, err = storage.GetExpectedChunksCount(ctx, session, "fresh")
		require.NoError(t, err)
	})
}

// An index is out of range only relative to a declared count, and the count can
// arrive after the chunk, so the check has to be able to look back over indices
// already recorded.
func TestFindChunkIndexAtOrAboveReportsTheLowestAndItsSender(t *testing.T) {
	const intruder = "@intruder:example.org"

	storage := newTestStorage(t)
	ctx := context.Background()
	now := time.Now()

	withSession(t, storage, func(session service.Session) {
		for _, index := range []uint32{0, 1} {
			_, err := storage.AddChunkIndex(ctx, session, "m1", index, testSender, now)
			require.NoError(t, err)
		}
		for _, index := range []uint32{9, 4} {
			_, err := storage.AddChunkIndex(ctx, session, "m1", index, intruder, now)
			require.NoError(t, err)
		}

		_, _, err := storage.FindChunkIndexAtOrAbove(ctx, session, "m1", 10)
		require.ErrorIs(t, err, service.ErrNotFound, "nothing is out of range for a count of 10")

		index, sender, err := storage.FindChunkIndexAtOrAbove(ctx, session, "m1", 2)
		require.NoError(t, err)
		require.Equal(t, uint32(4), index, "the lowest out-of-range index, not an arbitrary one")
		require.Equal(t, intruder, sender,
			"the sender stored with the chunk, not the one that declared the count")
	})
}

// A redelivery must not let a later account take the blame for an index, nor
// take it away from the account that first sent it.
func TestAddChunkIndexKeepsTheFirstSenderOfAnIndex(t *testing.T) {
	storage := newTestStorage(t)
	ctx := context.Background()
	now := time.Now()

	withSession(t, storage, func(session service.Session) {
		recorded, err := storage.AddChunkIndex(ctx, session, "m1", 5, testSender, now)
		require.NoError(t, err)
		require.True(t, recorded)

		recorded, err = storage.AddChunkIndex(ctx, session, "m1", 5, "@latecomer:example.org", now)
		require.NoError(t, err)
		require.False(t, recorded)

		_, sender, err := storage.FindChunkIndexAtOrAbove(ctx, session, "m1", 0)
		require.NoError(t, err)
		require.Equal(t, testSender, sender)
	})
}

func TestDeleteChunkIndicesAtOrAboveLeavesTheInRangeOnes(t *testing.T) {
	storage := newTestStorage(t)
	ctx := context.Background()
	now := time.Now()

	withSession(t, storage, func(session service.Session) {
		for _, index := range []uint32{0, 1, 4, 9} {
			_, err := storage.AddChunkIndex(ctx, session, "m1", index, testSender, now)
			require.NoError(t, err)
		}

		require.NoError(t, storage.DeleteChunkIndicesAtOrAbove(ctx, session, "m1", 2))

		_, _, err := storage.FindChunkIndexAtOrAbove(ctx, session, "m1", 2)
		require.ErrorIs(t, err, service.ErrNotFound, "the stray indices are gone")

		count, err := storage.CountChunkIndicesBelow(ctx, session, "m1", 2)
		require.NoError(t, err)
		require.Equal(t, uint32(2), count, "indices 0 and 1 must survive")

		// The message record itself is untouched: it still has chunks to come.
		_, err = storage.GetExpectedChunksCount(ctx, session, "m1")
		require.NoError(t, err)
	})
}

// The upgrade path, on a database built the way the deployed ones were rather
// than a fresh one. A fresh database runs both migrations and proves nothing
// about the databases that already exist.
//
// What migration 2 discards is in-flight reassembly tracking, and the rows it
// discards were already unusable: the previous code reused the wrong prepared
// statement when a chunk arrived and zeroed expected_chunks_count, so any
// message of three or more chunks could never reach its expected count, was
// never deleted, and sat in the table for good. The recovery path for a message
// still genuinely in flight is that its remaining chunks recreate the record,
// and it is swept if they never come.
func TestMigrationFromTheOriginalSchemaWithRowsInIt(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test-db")

	// Build the pre-migration state by hand: the original table, one stuck row
	// of the kind the old zeroing bug produced, and the bookkeeping that tells
	// golang-migrate migration 1 has been applied.
	func() {
		db, err := sql.Open("sqlite3", dbPath)
		require.NoError(t, err)
		defer func() { require.NoError(t, db.Close()) }()

		for _, stmt := range []string{
			`CREATE TABLE chunked_messages (
				message_id            VARCHAR(150) NOT NULL PRIMARY KEY,
				stored_chunks_count   INTEGER      NOT NULL,
				expected_chunks_count INTEGER      NOT NULL
			)`,
			`INSERT INTO chunked_messages VALUES ('stuck-in-flight', 2, 0)`,
			`CREATE TABLE schema_migrations (version uint64, dirty bool)`,
			`INSERT INTO schema_migrations VALUES (1, false)`,
		} {
			_, err := db.ExecContext(ctx, stmt)
			require.NoError(t, err, stmt)
		}
	}()

	storage, err := New(ctx, zap.NewNop().Sugar(), dbPath)
	require.NoError(t, err, "migration 2 must apply to a database already at version 1")
	t.Cleanup(func() { require.NoError(t, storage.Close()) })

	withSession(t, storage, func(session service.Session) {
		// The stuck row is gone, and its message id is free to be tracked again
		// the moment a chunk for it arrives.
		_, err := storage.GetExpectedChunksCount(ctx, session, "stuck-in-flight")
		require.ErrorIs(t, err, service.ErrNotFound)

		recorded, err := storage.AddChunkIndex(ctx, session, "stuck-in-flight", 1, testSender, time.Now())
		require.NoError(t, err)
		require.True(t, recorded, "a message still in flight is simply re-tracked")
	})

	// And the upgraded schema is the new one, sender column included.
	withSession(t, storage, func(session service.Session) {
		_, err := storage.AddChunkIndex(ctx, session, "m1", 7, "@intruder:example.org", time.Now())
		require.NoError(t, err)

		index, sender, err := storage.FindChunkIndexAtOrAbove(ctx, session, "m1", 2)
		require.NoError(t, err)
		require.Equal(t, uint32(7), index)
		require.Equal(t, "@intruder:example.org", sender)
	})
}
