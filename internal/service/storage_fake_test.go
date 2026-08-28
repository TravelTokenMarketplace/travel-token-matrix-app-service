// Copyright (C) 2022-2026, Travel Token Marketplace. All rights reserved.
// See the file LICENSE for licensing terms.

package service

import (
	"context"
	"errors"
	"time"
)

// fakeStorage is an in-memory MessageChunksStorage with the same semantics the
// sqlite implementation is required to have: chunk indices form a SET, and a
// repeated index is not recorded twice.
type fakeStorage struct {
	expected  map[string]uint32
	firstSeen map[string]time.Time
	indices   map[string]map[uint32]string // messageID -> index -> sender

	// failOn makes the named method return an error, to exercise the
	// "our failure, not the sender's" path.
	failOn string

	commits int
	aborts  int
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{
		expected:  map[string]uint32{},
		firstSeen: map[string]time.Time{},
		indices:   map[string]map[uint32]string{},
	}
}

var errStorageBroke = errors.New("storage broke")

func (f *fakeStorage) fail(method string) error {
	if f.failOn == method {
		return errStorageBroke
	}
	return nil
}

type fakeSession struct{}

func (fakeSession) Commit() error { return nil }
func (fakeSession) Abort() error  { return nil }

func (f *fakeStorage) NewSession(context.Context) (Session, error) {
	if err := f.fail("NewSession"); err != nil {
		return nil, err
	}
	return fakeSession{}, nil
}

func (f *fakeStorage) Commit(Session) error { f.commits++; return nil }
func (f *fakeStorage) Abort(Session)        { f.aborts++ }

func (f *fakeStorage) AddChunkIndex(
	_ context.Context, _ Session, messageID string, chunkIndex uint32, sender string, firstSeenAt time.Time,
) (bool, error) {
	if err := f.fail("AddChunkIndex"); err != nil {
		return false, err
	}
	if _, ok := f.indices[messageID]; !ok {
		f.indices[messageID] = map[uint32]string{}
		f.firstSeen[messageID] = firstSeenAt
		f.expected[messageID] = 0
	}
	if _, ok := f.indices[messageID][chunkIndex]; ok {
		// Like INSERT OR IGNORE: the first sender of an index keeps the row.
		return false, nil
	}
	f.indices[messageID][chunkIndex] = sender
	return true, nil
}

func (f *fakeStorage) SetExpectedChunksCount(
	_ context.Context, _ Session, messageID string, expectedChunksCount uint32,
) error {
	if err := f.fail("SetExpectedChunksCount"); err != nil {
		return err
	}
	f.expected[messageID] = expectedChunksCount
	return nil
}

func (f *fakeStorage) GetExpectedChunksCount(_ context.Context, _ Session, messageID string) (uint32, error) {
	if err := f.fail("GetExpectedChunksCount"); err != nil {
		return 0, err
	}
	if _, ok := f.indices[messageID]; !ok {
		return 0, ErrNotFound
	}
	return f.expected[messageID], nil
}

func (f *fakeStorage) CountChunkIndicesBelow(
	_ context.Context, _ Session, messageID string, limit uint32,
) (uint32, error) {
	if err := f.fail("CountChunkIndicesBelow"); err != nil {
		return 0, err
	}
	var count uint32
	for index := range f.indices[messageID] {
		if index < limit {
			count++
		}
	}
	return count, nil
}

func (f *fakeStorage) FindChunkIndexAtOrAbove(
	_ context.Context, _ Session, messageID string, limit uint32,
) (uint32, string, error) {
	if err := f.fail("FindChunkIndexAtOrAbove"); err != nil {
		return 0, "", err
	}
	found := false
	var lowest uint32
	for index := range f.indices[messageID] {
		if index >= limit && (!found || index < lowest) {
			lowest, found = index, true
		}
	}
	if !found {
		return 0, "", ErrNotFound
	}
	return lowest, f.indices[messageID][lowest], nil
}

func (f *fakeStorage) DeleteChunkIndicesAtOrAbove(
	_ context.Context, _ Session, messageID string, limit uint32,
) error {
	if err := f.fail("DeleteChunkIndicesAtOrAbove"); err != nil {
		return err
	}
	for index := range f.indices[messageID] {
		if index >= limit {
			delete(f.indices[messageID], index)
		}
	}
	return nil
}

func (f *fakeStorage) DeleteChunkedMessage(_ context.Context, _ Session, messageID string) error {
	if err := f.fail("DeleteChunkedMessage"); err != nil {
		return err
	}
	delete(f.indices, messageID)
	delete(f.expected, messageID)
	delete(f.firstSeen, messageID)
	return nil
}

func (f *fakeStorage) DeleteStalePartialMessages(_ context.Context, _ Session, cutoff time.Time) (int64, error) {
	if err := f.fail("DeleteStalePartialMessages"); err != nil {
		return 0, err
	}
	var deleted int64
	for messageID, seen := range f.firstSeen {
		if seen.Before(cutoff) {
			delete(f.indices, messageID)
			delete(f.expected, messageID)
			delete(f.firstSeen, messageID)
			deleted++
		}
	}
	return deleted, nil
}

// tracked reports whether the message still has a tracking record.
func (f *fakeStorage) tracked(messageID string) bool {
	_, ok := f.indices[messageID]
	return ok
}
