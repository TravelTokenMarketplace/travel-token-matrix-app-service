// Copyright (C) 2022-2026, Travel Token Marketplace. All rights reserved.
// See the file LICENSE for licensing terms.

package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TravelTokenMarketplace/travel-token-messenger-bot/v13/pkg/matrix"
	"go.uber.org/zap"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

const (
	// PartialMessageTTL bounds how long an incomplete message is tracked. It is
	// deliberately far longer than any plausible multi-chunk delivery, because
	// forgetting a message that is still arriving turns a slow delivery into a
	// sender wrongly accused of sending an out-of-range chunk. Its only job is
	// to stop unbounded growth of a table a remote peer can add rows to.
	PartialMessageTTL = 5 * time.Minute

	// PartialMessageSweepInterval is how often stale partial messages are
	// swept. Eviction is not latency-sensitive, so this is coarse on purpose.
	PartialMessageSweepInterval = time.Minute
)

var (
	_ Service = (*service)(nil)

	ErrNotFound = errors.New("not found")
)

type Service interface {
	ProcessEvents(ctx context.Context, events []event.Event) error

	// SweepStalePartialMessages drops tracking records for messages whose
	// remaining chunks never arrived, and reports how many were dropped.
	SweepStalePartialMessages(ctx context.Context, now time.Time) (int64, error)
}

// eventOutcome is what processing one event concluded about a sender.
//
// Which sender is a separate question from which event: an out-of-range chunk
// can only be recognised once the chunk count is declared, and the event that
// declares it may come from a different account than the one that sent the
// chunk. So outcomeMisbehaviour is always paired with the account actually
// responsible, which the caller bans — never simply the sender of whichever
// event happened to reveal the problem.
//
// It is deliberately separate from the error return. An error means this
// app-service failed — storage broke, a session could not be opened — and must
// fail the whole transaction so the homeserver retries it. An outcome is a
// judgement about a peer, and must never fail the transaction: a homeserver
// retries a transaction until it gets a 200 and holds that appservice's event
// stream while it does, so answering a peer's bad event with a 500 stalls
// every other sender behind it, indefinitely, for as long as the peer's event
// stays at the head of the queue.
type eventOutcome int

const (
	// outcomeAccepted: well-formed, and accounted for.
	outcomeAccepted eventOutcome = iota

	// outcomeDropped: this app-service could not make sense of the event, and
	// says so without blaming the sender. Used where the cause is as likely to
	// be ours as theirs, e.g. content that will not parse.
	outcomeDropped

	// outcomeMisbehaviour: the sender broke the protocol in a way only the
	// sender could cause. The event is discarded and the sender is a ban
	// candidate.
	outcomeMisbehaviour
)

func NewService(
	logger *zap.SugaredLogger,
	storage Storage,
) Service {
	return &service{
		logger:  logger,
		storage: storage,
		now:     time.Now,
		banUser: banUserNotImplemented,
	}
}

type service struct {
	logger  *zap.SugaredLogger
	storage Storage

	// now is a field so tests can drive time without sleeping.
	now func() time.Time

	// banUser is a field so a test can observe that a ban was actually
	// reached. The reachable triggers are documented on banUserNotImplemented.
	banUser func(ctx context.Context, userID id.UserID) error
}

func (s *service) ProcessEvents(ctx context.Context, events []event.Event) error {
	for _, evnt := range events {
		if evnt.Type.Type != matrix.EventTypeSignedMessage.Type && evnt.Type.Type != matrix.EventTypeMessageChunk.Type {
			s.logger.Debugf("Skipping event %s (%s) from %s, not a signed message or message chunk", evnt.ID, evnt.Type.Type, evnt.Sender)
			continue
		}

		// class is not transported and mautrix lib guess it
		// but here we don't use mautrix lib to receive events,
		// so we need to set it manually to allow mautrix to parse content correctly
		evnt.Type.Class = event.MessageEventType

		outcome, offender, err := s.processMessageEvent(ctx, &evnt)
		if err != nil {
			// Our failure, not the sender's: fail the transaction so the
			// homeserver redelivers it.
			return err
		}

		if outcome == outcomeMisbehaviour {
			if err := s.banUser(ctx, offender); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *service) processMessageEvent(ctx context.Context, evnt *event.Event) (eventOutcome, id.UserID, error) {
	s.logger.Debugf("Processing event %s (%s) from %s", evnt.ID, evnt.Type.Type, evnt.Sender)
	defer s.logger.Debugf("Finished processing event %s (%s) from %s", evnt.ID, evnt.Type.Type, evnt.Sender)

	if err := evnt.Content.ParseRaw(evnt.Type); err != nil {
		// Not attributed to the sender: the event class is not transported and
		// has to be set by hand above, so a parse failure is at least as likely
		// to be ours as theirs. Dropping it keeps the transaction succeeding,
		// which matters more than punishing a sender we cannot be sure about.
		s.logger.Warnf("Event %s from %s: dropping, failed to parse content: %v", evnt.ID, evnt.Sender, err)
		return outcomeDropped, "", nil
	}

	switch eventContent := evnt.Content.Parsed.(type) {
	case *matrix.SignedMessageEventContent:
		return s.processSignedMessageEvent(ctx, eventContent, evnt)
	case *matrix.MessageChunkEventContent:
		return s.processMessageChunkEvent(ctx, eventContent, evnt)
	}

	s.logger.Warnf("Event %s from %s: dropping, unsupported event type %s", evnt.ID, evnt.Sender, evnt.Type.Type)
	return outcomeDropped, "", nil
}

// processSignedMessageEvent handles the event that opens a message. It carries
// the chunk count, and is itself chunk index 0.
func (s *service) processSignedMessageEvent(
	ctx context.Context,
	eventContent *matrix.SignedMessageEventContent,
	evnt *event.Event,
) (eventOutcome, id.UserID, error) {
	if err := eventContent.Verify(); err != nil {
		s.logger.Infof("Event %s, message %s from %s: invalid event content: %v", evnt.ID, eventContent.MessageID, evnt.Sender, err)
		return outcomeMisbehaviour, evnt.Sender, nil
	}

	// A single-chunk message is complete on arrival and never needs tracking.
	if eventContent.ChunksCount == 1 {
		return outcomeAccepted, "", nil
	}

	return s.recordChunk(ctx, eventContent.MessageID, 0, eventContent.ChunksCount, evnt)
}

func (s *service) processMessageChunkEvent(
	ctx context.Context,
	eventContent *matrix.MessageChunkEventContent,
	evnt *event.Event,
) (eventOutcome, id.UserID, error) {
	if err := eventContent.Verify(); err != nil {
		s.logger.Infof("Event %s, message %s from %s: invalid event content: %v", evnt.ID, eventContent.MessageID, evnt.Sender, err)
		return outcomeMisbehaviour, evnt.Sender, nil
	}

	// A chunk event carries no chunk count; only the signed message does, and
	// it may not have arrived yet. 0 means "not yet known".
	return s.recordChunk(ctx, eventContent.MessageID, eventContent.ChunkIndex, 0, evnt)
}

// recordChunk records one arrived chunk index and decides whether the message
// is now complete.
//
// expectedChunksCount is the count this event declares, or 0 if it declares
// none. Completeness is "every index in 0..expected-1 has been recorded", not
// "expected arrivals have been counted" — the difference is the whole point of
// tracking indices, because a redelivered chunk satisfies the second and not
// the first.
func (s *service) recordChunk(
	ctx context.Context,
	messageID string,
	chunkIndex uint32,
	expectedChunksCount uint32,
	evnt *event.Event,
) (eventOutcome, id.UserID, error) {
	session, err := s.storage.NewSession(ctx)
	if err != nil {
		err = fmt.Errorf("failed to create storage session: %w", err)
		s.logger.Errorf("Event %s, message %s: %v", evnt.ID, messageID, err)
		return outcomeAccepted, "", err
	}
	defer s.storage.Abort(session)

	recorded, err := s.storage.AddChunkIndex(ctx, session, messageID, chunkIndex, string(evnt.Sender), s.now())
	if err != nil {
		err = fmt.Errorf("failed to record chunk index: %w", err)
		s.logger.Errorf("Event %s, message %s: %v", evnt.ID, messageID, err)
		return outcomeAccepted, "", err
	}
	if !recorded {
		// The same index arriving twice is ordinary Matrix redelivery, not the
		// sender's doing. It used to inflate the arrival count; now it changes
		// nothing at all.
		s.logger.Debugf("Event %s, message %s: chunk %d already recorded, ignoring redelivery", evnt.ID, messageID, chunkIndex)
	}

	if expectedChunksCount != 0 {
		if err := s.storage.SetExpectedChunksCount(ctx, session, messageID, expectedChunksCount); err != nil {
			err = fmt.Errorf("failed to set expected chunks count: %w", err)
			s.logger.Errorf("Event %s, message %s: %v", evnt.ID, messageID, err)
			return outcomeAccepted, "", err
		}
	} else {
		expectedChunksCount, err = s.storage.GetExpectedChunksCount(ctx, session, messageID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			err = fmt.Errorf("failed to get expected chunks count: %w", err)
			s.logger.Errorf("Event %s, message %s: %v", evnt.ID, messageID, err)
			return outcomeAccepted, "", err
		}
	}

	// Still waiting on the signed message that declares the count. The chunk is
	// recorded and will be judged once the count is known.
	if expectedChunksCount == 0 {
		return outcomeAccepted, "", s.storage.Commit(session)
	}

	outcome, offender, err := s.reportOutOfRangeChunks(ctx, session, messageID, expectedChunksCount, evnt)
	if err != nil {
		return outcomeAccepted, "", err
	}

	presentChunks, err := s.storage.CountChunkIndicesBelow(ctx, session, messageID, expectedChunksCount)
	if err != nil {
		err = fmt.Errorf("failed to count chunk indices: %w", err)
		s.logger.Errorf("Event %s, message %s: %v", evnt.ID, messageID, err)
		return outcomeAccepted, "", err
	}

	if presentChunks == expectedChunksCount {
		if err := s.storage.DeleteChunkedMessage(ctx, session, messageID); err != nil {
			err = fmt.Errorf("failed to delete chunked message: %w", err)
			s.logger.Errorf("Event %s, message %s: %v", evnt.ID, messageID, err)
			return outcomeAccepted, "", err
		}
	}

	return outcome, offender, s.storage.Commit(session)
}

// reportOutOfRangeChunks looks for a recorded index that the now-known chunk
// count says cannot belong to the message, and reports whoever sent it.
//
// It searches storage rather than checking only the index in hand, because the
// two events involved are not ordered: a chunk can arrive before the signed
// message that declares the count, and until that count is known there is
// nothing to judge the index against. Checking only the current event let a
// sender launder an out-of-range chunk by simply sending it first — which is
// the order a sender doing it on purpose would pick.
//
// The offending indices are deleted once reported. They can never help complete
// the message (completeness counts only indices below the declared count), so
// nothing is lost, and it stops the same stray index being re-reported for
// every subsequent chunk of the same message.
func (s *service) reportOutOfRangeChunks(
	ctx context.Context,
	session Session,
	messageID string,
	expectedChunksCount uint32,
	evnt *event.Event,
) (eventOutcome, id.UserID, error) {
	outOfRangeIndex, sender, err := s.storage.FindChunkIndexAtOrAbove(ctx, session, messageID, expectedChunksCount)
	if errors.Is(err, ErrNotFound) {
		return outcomeAccepted, "", nil
	}
	if err != nil {
		err = fmt.Errorf("failed to look for out-of-range chunk indices: %w", err)
		s.logger.Errorf("Event %s, message %s: %v", evnt.ID, messageID, err)
		return outcomeAccepted, "", err
	}

	// The sender comes from the stored row, not from evnt: the account that
	// declared the count is not necessarily the one that sent the stray index.
	s.logger.Infof("Event %s, message %s from %s: chunk index %d is beyond the declared chunk count %d",
		evnt.ID, messageID, sender, outOfRangeIndex, expectedChunksCount)

	if err := s.storage.DeleteChunkIndicesAtOrAbove(ctx, session, messageID, expectedChunksCount); err != nil {
		err = fmt.Errorf("failed to delete out-of-range chunk indices: %w", err)
		s.logger.Errorf("Event %s, message %s: %v", evnt.ID, messageID, err)
		return outcomeAccepted, "", err
	}

	return outcomeMisbehaviour, id.UserID(sender), nil
}

func (s *service) SweepStalePartialMessages(ctx context.Context, now time.Time) (int64, error) {
	session, err := s.storage.NewSession(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to create storage session: %w", err)
	}
	defer s.storage.Abort(session)

	swept, err := s.storage.DeleteStalePartialMessages(ctx, session, now.Add(-PartialMessageTTL))
	if err != nil {
		return 0, fmt.Errorf("failed to delete stale partial messages: %w", err)
	}
	if swept > 0 {
		s.logger.Warnf("Evicted %d incomplete message(s) older than %s", swept, PartialMessageTTL)
	}
	return swept, s.storage.Commit(session)
}

// banUserNotImplemented is the standing no-op ban action.
//
// TODO @evlekht implement (next ticket) // persist with db, make it durable? not just call it from event receiver?
//
// What reaches it is now a deliberate, closed set. It is called for
// outcomeMisbehaviour and nothing else, and outcomeMisbehaviour is returned for
// exactly two things:
//
//   - event content that fails structural verification (zero chunk count, zero
//     chunk index, empty data or message id);
//   - a chunk index at or beyond the chunk count the message declared, whether
//     the index arrived after the count or before it.
//
// Both are things only the sender can cause. Content that will not parse is
// deliberately NOT in the set, because the event class has to be reconstructed
// on this side and a parse failure is as likely to be ours.
//
// The user banned is the one that sent the offending chunk, which for the
// out-of-range trigger is read back from storage rather than taken from the
// event being processed. A message id is chosen by its sender and nothing binds
// one to a single account, so blaming whoever declared the count would let one
// peer get another banned by planting a stray index under its message id.
//
// Previously nothing could reach this at all: the verification branch returned
// a non-nil error alongside its ban verdict, and the caller checked the error
// first and abandoned the whole transaction, so the ban was unreachable dead
// code. The one branch that did reach it fired on a redelivered chunk — so the
// only ban that could ever have happened would have punished a compliant peer.
func banUserNotImplemented(_ context.Context, _ id.UserID) error {
	return nil
}
