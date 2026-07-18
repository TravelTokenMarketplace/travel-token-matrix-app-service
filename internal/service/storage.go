// Copyright (C) 2022-2026, Travel Token Marketplace. All rights reserved.
// See the file LICENSE for licensing terms.

package service

import (
	"context"
)

type Storage interface {
	SessionHandler
	MessageChunksStorage
}

type MessageChunksStorage interface {
	GetChunksCount(
		ctx context.Context,
		session Session,
		messageID string,
	) (
		storedChunksCount uint32,
		expectedChunksCount uint32,
		err error,
	)

	AddFirstChunk(
		ctx context.Context,
		session Session,
		messageID string,
		expectedChunksCount uint32,
		storedChunksCount uint32,
	) error

	UpdateChunksCount(
		ctx context.Context,
		session Session,
		messageID string,
		storedChunksCount uint32,
	) error

	DeleteChunkedMessage(
		ctx context.Context,
		session Session,
		messageID string,
	) error
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
