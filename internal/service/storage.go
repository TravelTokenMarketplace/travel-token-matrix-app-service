// Copyright (C) 2022-2026, Travel Token Marketplace. All rights reserved.
// See the file LICENSE for licensing terms.

package service

import (
	"context"
	"time"
)

type Storage interface {
	SessionHandler
	MessageChunksStorage
}

// MessageChunksStorage tracks which chunks of a multi-chunk message have been
// seen.
//
// It records the SET of arrived chunk indices rather than a count of arrivals.
// Matrix redelivers events over a lossy link, and a count cannot tell a
// redelivery from a new chunk: counting let a duplicate stand in for a chunk
// that never arrived, and let a compliant sender look like it had sent more
// chunks than it declared.
type MessageChunksStorage interface {
	// AddChunkIndex records that chunkIndex of messageID has arrived, creating
	// the message's tracking record if this is the first chunk seen for it.
	// firstSeenAt is used only when the record is created.
	//
	// It reports whether the index was newly recorded. False means this exact
	// index had already arrived — a redelivery, which is normal and not the
	// sender's fault.
	AddChunkIndex(
		ctx context.Context,
		session Session,
		messageID string,
		chunkIndex uint32,
		firstSeenAt time.Time,
	) (recorded bool, err error)

	// SetExpectedChunksCount records how many chunks the message has, which is
	// known only from the signed message. The tracking record must already
	// exist.
	SetExpectedChunksCount(
		ctx context.Context,
		session Session,
		messageID string,
		expectedChunksCount uint32,
	) error

	// GetExpectedChunksCount returns the expected chunk count for messageID.
	// It returns 0 when the signed message carrying the count has not arrived
	// yet, and ErrNotFound when nothing is tracked for messageID at all.
	GetExpectedChunksCount(
		ctx context.Context,
		session Session,
		messageID string,
	) (uint32, error)

	// CountChunkIndicesBelow returns how many distinct indices strictly below
	// limit have been recorded for messageID. Because indices are unique per
	// message, a result equal to limit means every index in 0..limit-1 is
	// present — which is what completeness actually is, as opposed to having
	// accumulated limit arrivals.
	CountChunkIndicesBelow(
		ctx context.Context,
		session Session,
		messageID string,
		limit uint32,
	) (uint32, error)

	// DeleteChunkedMessage removes a message's tracking record and every chunk
	// index recorded for it.
	DeleteChunkedMessage(
		ctx context.Context,
		session Session,
		messageID string,
	) error

	// DeleteStalePartialMessages removes tracking records first seen before
	// cutoff, together with their recorded indices, and reports how many
	// messages were removed. Without it the table grows without bound whenever
	// a peer starts a multi-chunk message and never finishes it.
	DeleteStalePartialMessages(
		ctx context.Context,
		session Session,
		cutoff time.Time,
	) (int64, error)
}

type SessionHandler interface {
	NewSession(ctx context.Context) (Session, error)
	Commit(session Session) error
	Abort(session Session)
}

type Session interface {
	Commit() error
	Abort() error
}
