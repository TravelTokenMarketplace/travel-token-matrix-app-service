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
	// AddChunkIndex records that chunkIndex of messageID has arrived from
	// sender, creating the message's tracking record if this is the first chunk
	// seen for it. firstSeenAt is used only when the record is created.
	//
	// It reports whether the index was newly recorded. False means this exact
	// index had already arrived — a redelivery, which is normal and not the
	// sender's fault. The sender stored is therefore the first one to send that
	// index; a redelivery does not overwrite it.
	AddChunkIndex(
		ctx context.Context,
		session Session,
		messageID string,
		chunkIndex uint32,
		sender string,
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

	// FindChunkIndexAtOrAbove returns one recorded index at or beyond limit for
	// messageID, together with the account that sent it, and ErrNotFound when
	// there is none.
	//
	// An index is out of range only relative to a declared chunk count, and the
	// count arrives in its own event which a chunk can overtake. So a chunk
	// cannot always be judged when it is seen, and the check has to be able to
	// look back over what was already recorded once the count is known.
	//
	// It returns the sender because the account that declares the count need
	// not be the account that sent the offending index — a message id is picked
	// by the sender and nothing binds one to a single account.
	FindChunkIndexAtOrAbove(
		ctx context.Context,
		session Session,
		messageID string,
		limit uint32,
	) (chunkIndex uint32, sender string, err error)

	// DeleteChunkIndicesAtOrAbove removes recorded indices at or beyond limit
	// for messageID. Out-of-range indices are dropped once they have been
	// reported, so the same one is not reported again for every later chunk of
	// the same message.
	DeleteChunkIndicesAtOrAbove(
		ctx context.Context,
		session Session,
		messageID string,
		limit uint32,
	) error

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
