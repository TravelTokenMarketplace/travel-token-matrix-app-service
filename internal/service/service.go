// Copyright (C) 2022-2026, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/TravelTokenMarketplace/travel-token-messenger-bot/v13/pkg/matrix"
	"go.uber.org/zap"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

var (
	_ Service = (*service)(nil)

	ErrNotFound = errors.New("not found")
)

type Service interface {
	ProcessEvents(ctx context.Context, events []event.Event) error
}

func NewService(
	logger *zap.SugaredLogger,
	storage Storage,
) Service {
	return &service{
		logger:  logger,
		storage: storage,
	}
}

type service struct {
	logger  *zap.SugaredLogger
	storage Storage
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

		banSender, err := s.processMessageEvent(ctx, &evnt)
		if err != nil {
			return err
		}

		if banSender {
			if err := s.banUser(ctx, evnt.Sender); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *service) processMessageEvent(ctx context.Context, evnt *event.Event) (bool, error) {
	s.logger.Debugf("Processing event %s (%s) from %s", evnt.ID, evnt.Type.Type, evnt.Sender)
	defer s.logger.Debugf("Finished processing event %s (%s) from %s", evnt.ID, evnt.Type.Type, evnt.Sender)

	if err := evnt.Content.ParseRaw(evnt.Type); err != nil { // TODO@ type.Class is 0, not 1! failed to guess class, I think!
		s.logger.Errorf("Failed to parse event content: %v", err)
		// TODO @evlekht ban users for malformed events? e.g. we fail to parse? might be server/lib fault, though it shouldn't just pop up out of nowhere
		return false, err
	}

	switch eventContent := evnt.Content.Parsed.(type) {
	case *matrix.SignedMessageEventContent:
		return s.processSignedMessageEvent(ctx, eventContent, evnt.Sender, evnt.ID)
	case *matrix.MessageChunkEventContent:
		return s.processMessageChunkEvent(ctx, eventContent, evnt.Sender, evnt.ID)
	}

	return false, fmt.Errorf("unsupported event type: %s", evnt.Type.Type)
}

func (s *service) processSignedMessageEvent(ctx context.Context, eventContent *matrix.SignedMessageEventContent, senderBotUserID id.UserID, eventID id.EventID) (bool, error) {
	if err := eventContent.Verify(); err != nil {
		s.logger.Infof("Event %s, message %s from %s: invalid event content: %v", eventID, eventContent.MessageID, senderBotUserID, err)
		return true, err
	}

	session, err := s.storage.NewSession(ctx)
	if err != nil {
		err = fmt.Errorf("failed to create storage session: %w", err)
		s.logger.Errorf("Event %s, message %s: %v", eventID, eventContent.MessageID, err)
		return false, err
	}
	defer s.storage.Abort(session)

	storedChunksCount, _, err := s.storage.GetChunksCount(ctx, session, eventContent.MessageID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		err = fmt.Errorf("failed to get chunks count: %w", err)
		s.logger.Errorf("Event %s, message %s: %v", eventID, eventContent.MessageID, err)
		return false, err
	}

	newChunksCount := storedChunksCount + 1

	if newChunksCount > eventContent.ChunksCount {
		s.logger.Infof("Event %s, message %s: received more chunks than expected (%d > %d)", eventID, eventContent.MessageID, newChunksCount, eventContent.ChunksCount)
		return true, nil
	}

	if newChunksCount == eventContent.ChunksCount {
		if newChunksCount > 1 { // more than one chunk -> delete chunked message record from db
			if err := s.storage.DeleteChunkedMessage(ctx, session, eventContent.MessageID); err != nil {
				err = fmt.Errorf("failed to delete chunked message: %w", err)
				s.logger.Errorf("Event %s, message %s: %v", eventID, eventContent.MessageID, err)
				return false, err
			}
		}
	} else {
		if err := s.storage.AddFirstChunk(ctx, session, eventContent.MessageID, eventContent.ChunksCount, newChunksCount); err != nil {
			err = fmt.Errorf("failed to add first chunk: %w", err)
			s.logger.Errorf("Event %s, message %s: %v", eventID, eventContent.MessageID, err)
			return false, err
		}
	}

	return false, s.storage.Commit(session)
}

func (s *service) processMessageChunkEvent(ctx context.Context, eventContent *matrix.MessageChunkEventContent, senderBotUserID id.UserID, eventID id.EventID) (bool, error) {
	if err := eventContent.Verify(); err != nil {
		s.logger.Infof("Event %s, message %s from %s: invalid event content: %v", eventID, eventContent.MessageID, senderBotUserID, err)
		return true, err
	}

	session, err := s.storage.NewSession(ctx)
	if err != nil {
		err = fmt.Errorf("failed to create storage session: %w", err)
		s.logger.Errorf("Event %s, message %s: %v", eventID, eventContent.MessageID, err)
		return false, err
	}
	defer s.storage.Abort(session)

	storedChunksCount, expectedChunksCount, err := s.storage.GetChunksCount(ctx, session, eventContent.MessageID)
	if err != nil {
		err = fmt.Errorf("failed to get chunks count: %w", err)
		s.logger.Errorf("Event %s, message %s: %v", eventID, eventContent.MessageID, err)
		return false, err
	}

	newChunksCount := storedChunksCount + 1

	if newChunksCount == expectedChunksCount {
		if err := s.storage.DeleteChunkedMessage(ctx, session, eventContent.MessageID); err != nil {
			err = fmt.Errorf("failed to delete chunked message: %w", err)
			s.logger.Errorf("Event %s, message %s: %v", eventID, eventContent.MessageID, err)
			return false, err
		}
	} else {
		if err := s.storage.UpdateChunksCount(ctx, session, eventContent.MessageID, newChunksCount); err != nil {
			err = fmt.Errorf("failed to update chunks count: %w", err)
			s.logger.Errorf("Event %s, message %s: %v", eventID, eventContent.MessageID, err)
			return false, err
		}
	}
	return false, s.storage.Commit(session)
}

// TODO @evlekht implement (next ticket) // persist with db, make it durable? not just call it from event receiver?
func (s *service) banUser(_ context.Context, _ id.UserID) error {
	return nil
}
