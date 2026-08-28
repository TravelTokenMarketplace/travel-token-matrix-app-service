// Copyright (C) 2022-2026, Travel Token Marketplace. All rights reserved.
// See the file LICENSE for licensing terms.

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/TravelTokenMarketplace/travel-token-messenger-bot/v13/pkg/matrix"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

const (
	testMessageID = "message-1"
	testSender    = id.UserID("@bot:example.org")
)

var testNow = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

func newTestService(storage *fakeStorage) (*service, *int) {
	bans := 0
	svc := &service{
		logger:  zap.NewNop().Sugar(),
		storage: storage,
		now:     func() time.Time { return testNow },
		banUser: func(context.Context, id.UserID) error { bans++; return nil },
	}
	return svc, &bans
}

// rawContent renders the event body the way the homeserver delivers it:
// ParseRaw unmarshals Content.VeryRaw, so that is what a test has to populate.
func rawContent(fields map[string]any) event.Content {
	encoded, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return event.Content{VeryRaw: encoded}
}

func signedMessageEvent(messageID string, chunksCount uint32) event.Event {
	return event.Event{
		ID:     id.EventID("$signed-" + messageID),
		Sender: testSender,
		Type:   matrix.EventTypeSignedMessage,
		Content: rawContent(map[string]any{
			"MessageID":   messageID,
			"Data":        []byte("chunk-0"),
			"ChunksCount": chunksCount,
		}),
	}
}

func chunkEvent(messageID string, chunkIndex uint32) event.Event {
	return event.Event{
		ID:     id.EventID("$chunk-" + messageID),
		Sender: testSender,
		Type:   matrix.EventTypeMessageChunk,
		Content: rawContent(map[string]any{
			"MessageID":  messageID,
			"Data":       []byte("chunk-n"),
			"ChunkIndex": chunkIndex,
		}),
	}
}

// The defect this whole change exists for: a chunk that Matrix redelivers must
// not stand in for a chunk that never arrived.
func TestRedeliveredChunkDoesNotCompleteAMessageWithAHole(t *testing.T) {
	storage := newFakeStorage()
	svc, bans := newTestService(storage)

	// A three-chunk message: the signed message (index 0), then index 1 twice.
	// Counting arrivals, that is three — enough to call the message complete
	// and drop it, even though index 2 has never been seen.
	require.NoError(t, svc.ProcessEvents(context.Background(), []event.Event{
		signedMessageEvent(testMessageID, 3),
		chunkEvent(testMessageID, 1),
		chunkEvent(testMessageID, 1),
	}))

	require.True(t, storage.tracked(testMessageID),
		"message must still be tracked: index 2 has not arrived")
	require.Zero(t, *bans, "a redelivery is the network's doing, not the sender's")

	// The genuine trailing chunk still finds its message and completes it.
	require.NoError(t, svc.ProcessEvents(context.Background(), []event.Event{chunkEvent(testMessageID, 2)}))
	require.False(t, storage.tracked(testMessageID), "message is complete and should be forgotten")
}

// The trailing chunk of a message whose record was dropped used to fail
// outright, which aborted the batch and answered the homeserver with a 500.
func TestChunkForAnUnknownMessageIsNotAnError(t *testing.T) {
	storage := newFakeStorage()
	svc, bans := newTestService(storage)

	require.NoError(t, svc.ProcessEvents(context.Background(), []event.Event{chunkEvent("never-announced", 4)}))
	require.Zero(t, *bans)
	require.True(t, storage.tracked("never-announced"),
		"the chunk is kept: the signed message that declares the count may still be on its way")
}

// Chunks can overtake the signed message that declares the chunk count.
func TestChunkArrivingBeforeTheSignedMessageIsKept(t *testing.T) {
	storage := newFakeStorage()
	svc, bans := newTestService(storage)

	require.NoError(t, svc.ProcessEvents(context.Background(), []event.Event{chunkEvent(testMessageID, 1)}))
	require.True(t, storage.tracked(testMessageID))

	require.NoError(t, svc.ProcessEvents(context.Background(), []event.Event{signedMessageEvent(testMessageID, 2)}))
	require.False(t, storage.tracked(testMessageID), "both indices present, message is complete")
	require.Zero(t, *bans)
}

func TestChunkIndexBeyondTheDeclaredCountIsMisbehaviour(t *testing.T) {
	storage := newFakeStorage()
	svc, bans := newTestService(storage)

	require.NoError(t, svc.ProcessEvents(context.Background(), []event.Event{
		signedMessageEvent(testMessageID, 2),
		chunkEvent(testMessageID, 7),
	}))
	require.Equal(t, 1, *bans, "only the sender can produce an out-of-range index")
}

// The ban path used to be unreachable: the verification branch returned an
// error next to its verdict, and the caller checked the error first.
func TestInvalidContentBansTheSenderAndTheBatchContinues(t *testing.T) {
	storage := newFakeStorage()
	svc, bans := newTestService(storage)

	// ChunksCount 0 fails structural verification.
	require.NoError(t, svc.ProcessEvents(context.Background(), []event.Event{
		signedMessageEvent("bad-message", 0),
		signedMessageEvent(testMessageID, 2),
	}))

	require.Equal(t, 1, *bans, "the ban must actually be reached")
	require.True(t, storage.tracked(testMessageID),
		"the event after the bad one must still have been processed")
}

// Content that will not parse is dropped without accusing anyone, because the
// event class has to be reconstructed on this side.
func TestUnparseableContentIsDroppedWithoutABan(t *testing.T) {
	storage := newFakeStorage()
	svc, bans := newTestService(storage)

	broken := signedMessageEvent(testMessageID, 2)
	broken.Content = rawContent(map[string]any{
		"MessageID":   testMessageID,
		"Data":        []byte("chunk-0"),
		"ChunksCount": "not a number",
	})

	require.NoError(t, svc.ProcessEvents(context.Background(), []event.Event{broken}))
	require.Zero(t, *bans)
	require.False(t, storage.tracked(testMessageID))
}

// Our own failures must still fail the transaction, so the homeserver retries.
func TestStorageFailureFailsTheTransaction(t *testing.T) {
	for _, method := range []string{"NewSession", "AddChunkIndex", "CountChunkIndicesBelow"} {
		t.Run(method, func(t *testing.T) {
			storage := newFakeStorage()
			storage.failOn = method
			svc, bans := newTestService(storage)

			err := svc.ProcessEvents(context.Background(), []event.Event{signedMessageEvent(testMessageID, 2)})
			require.ErrorIs(t, err, errStorageBroke)
			require.Zero(t, *bans, "our failure must never be charged to the sender")
		})
	}
}

func TestSingleChunkMessageIsNeverTracked(t *testing.T) {
	storage := newFakeStorage()
	svc, bans := newTestService(storage)

	require.NoError(t, svc.ProcessEvents(context.Background(), []event.Event{signedMessageEvent(testMessageID, 1)}))
	require.False(t, storage.tracked(testMessageID))
	require.Zero(t, *bans)
	require.Zero(t, storage.commits, "a complete single-chunk message needs no storage session at all")
}

func TestUnrelatedEventTypesAreSkipped(t *testing.T) {
	storage := newFakeStorage()
	svc, bans := newTestService(storage)

	require.NoError(t, svc.ProcessEvents(context.Background(), []event.Event{
		{ID: "$other", Sender: testSender, Type: event.EventMessage},
	}))
	require.Zero(t, *bans)
	require.Zero(t, storage.commits)
}

// A peer that starts messages and never finishes them must not grow the
// tracking table without bound.
func TestSweepStalePartialMessages(t *testing.T) {
	storage := newFakeStorage()
	svc, _ := newTestService(storage)

	require.NoError(t, svc.ProcessEvents(context.Background(), []event.Event{signedMessageEvent(testMessageID, 3)}))
	require.True(t, storage.tracked(testMessageID))

	// Still inside the TTL: a slow delivery must not be evicted.
	swept, err := svc.SweepStalePartialMessages(context.Background(), testNow.Add(PartialMessageTTL-time.Second))
	require.NoError(t, err)
	require.Zero(t, swept)
	require.True(t, storage.tracked(testMessageID))

	swept, err = svc.SweepStalePartialMessages(context.Background(), testNow.Add(PartialMessageTTL+time.Second))
	require.NoError(t, err)
	require.Equal(t, int64(1), swept)
	require.False(t, storage.tracked(testMessageID))
}
