// Copyright (C) 2022-2026, Travel Token Marketplace. All rights reserved.
// See the file LICENSE for licensing terms.

package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/TravelTokenMarketplace/travel-token-matrix-app-service/internal/service"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

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
		recorded, err := storage.AddChunkIndex(ctx, session, "m1", 1, now)
		require.NoError(t, err)
		require.True(t, recorded, "first sighting of an index is recorded")

		recorded, err = storage.AddChunkIndex(ctx, session, "m1", 1, now)
		require.NoError(t, err)
		require.False(t, recorded, "the same index again is a redelivery, not a new chunk")

		recorded, err = storage.AddChunkIndex(ctx, session, "m1", 2, now)
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
			_, err := storage.AddChunkIndex(ctx, session, "m1", index, now)
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

		_, err = storage.AddChunkIndex(ctx, session, "m1", 1, now)
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
		_, err := storage.AddChunkIndex(ctx, session, "m1", 0, start)
		require.NoError(t, err)
		_, err = storage.AddChunkIndex(ctx, session, "m1", 1, start.Add(10*time.Minute))
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
		_, err := storage.AddChunkIndex(ctx, session, "m1", 0, now)
		require.NoError(t, err)
		_, err = storage.AddChunkIndex(ctx, session, "m1", 1, now)
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
		_, err := storage.AddChunkIndex(ctx, session, "old", 0, now.Add(-time.Hour))
		require.NoError(t, err)
		_, err = storage.AddChunkIndex(ctx, session, "fresh", 0, now)
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
