package openai

import (
	"context"
	"errors"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// conversation.item.delete removes an item from the server's conversation;
// conversation.item.deleted acknowledges it.
const (
	realtimeEventItemDelete  = "conversation.item.delete"
	realtimeEventItemDeleted = "conversation.item.deleted"
)

// conversationItemDeleteEvent (conversation.item.delete) removes an item from
// the conversation the model answers from.
type conversationItemDeleteEvent struct {
	ClientEvent
	ItemID string `json:"item_id"`
}

// conversationItemDeletedEvent (conversation.item.deleted) acknowledges a
// conversation.item.delete.
type conversationItemDeletedEvent struct {
	ServerEvent
	ItemID string `json:"item_id"`
}

// turnLedger is what the session knows of each turn's items, to forget one
// (ForgetTurn). Guarded by the session's recvMu.
type turnLedger struct {
	// user is the caller's latest audio item (input_audio_buffer.committed):
	// the input a response created now answers. A typed turn clears it.
	user string
	// genUser is the caller's item each response answers, and genItems the
	// items each completed response output, by generation, for the responses
	// the session remembers (maxRespGens).
	genUser  map[int64]string
	genItems map[int64][]string
	// pendingGen is a response to forget when it ends (in progress, or not
	// created yet), with pendingUser, the caller's item it answers.
	pendingGen  int64
	pendingUser string
	// deleted holds the items already deleted, so none is deleted twice.
	deleted map[string]bool
	// deletes tracks the conversation.item.delete events awaiting the
	// server's answer, so one it rejects (the item is gone) is logged, not
	// reported as a session error.
	deletes truncateTracker
}

// ForgetTurn implements providers.TurnForgetter: it deletes, with
// conversation.item.delete, the caller's audio item that the response
// numbered generation answers, and the items that response output, so the
// model no longer has the turn in its context.
//
// A completed response is forgotten at once. One in progress, or not created
// yet, is forgotten when its response.done arrives, so none of its items is
// missed; one not created yet when the caller starts a new turn is not the
// blocked turn's reply, and only the caller's blocked item is deleted then.
// Earlier responses of a tool turn, and the tool results, are left: the
// conversation keeps each call with its result. Generation 0 is the response
// in progress, or the next one. A turn is deleted once, and a delete the
// server rejects (the item is already gone) is not a session error.
func (s *RealtimeSession) ForgetTurn(_ context.Context, generation int64) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errors.New(errSessionClosed)
	}

	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	if s.emitClosed {
		return errors.New(errSessionClosed)
	}
	target := s.targetGeneration(generation)
	l := &s.turns
	if target > s.gen || (target == s.gen && s.responseActive) {
		user := l.user
		if u, known := l.genUser[target]; known {
			user = u
		}
		l.pendingGen, l.pendingUser = target, user
		return nil
	}
	s.deleteItems(append([]string{l.genUser[target]}, l.genItems[target]...))
	return nil
}

// noteUserItem records the caller's new turn: item is their audio item, or
// "" for a typed one. A forget pending on a response not created yet is the
// last turn's: only its caller's item is deleted. Called with recvMu held.
func (s *RealtimeSession) noteUserItem(item string) {
	l := &s.turns
	if l.pendingGen > s.gen && l.pendingUser != item {
		s.deleteItems([]string{l.pendingUser})
		l.pendingGen, l.pendingUser = 0, ""
	}
	l.user = item
}

// startTypedTurn records that the caller typed a turn (SendText).
func (s *RealtimeSession) startTypedTurn() {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	s.noteUserItem("")
}

// noteResponseStarted records the caller's item the response numbered gen
// answers, and forgets what the session knew of the response whose number it
// no longer remembers (evicted). Called with recvMu held.
func (s *RealtimeSession) noteResponseStarted(gen, evicted int64) {
	l := &s.turns
	if l.genUser == nil {
		l.genUser = map[int64]string{}
		l.genItems = map[int64][]string{}
	}
	if evicted != 0 {
		delete(l.genUser, evicted)
		delete(l.genItems, evicted)
	}
	l.genUser[gen] = l.user
}

// noteResponseDone records the items the response numbered gen output, and
// forgets it when it was pending. Called with recvMu held.
func (s *RealtimeSession) noteResponseDone(gen int64, info *ResponseInfo) {
	if gen == 0 {
		return
	}
	l := &s.turns
	var items []string
	for i := range info.Output {
		items = append(items, info.Output[i].ID)
	}
	if l.genItems != nil {
		l.genItems[gen] = items
	}
	if l.pendingGen != gen {
		return
	}
	s.deleteItems(append([]string{l.pendingUser}, items...))
	l.pendingGen, l.pendingUser = 0, ""
}

// deleteItems deletes each item not yet deleted, in order. Called with recvMu
// held.
func (s *RealtimeSession) deleteItems(items []string) {
	l := &s.turns
	for _, item := range items {
		if item == "" || l.deleted[item] {
			continue
		}
		if l.deleted == nil {
			l.deleted = map[string]bool{}
		}
		l.deleted[item] = true
		event := conversationItemDeleteEvent{
			ClientEvent: ClientEvent{EventID: s.nextEventID(), Type: realtimeEventItemDelete},
			ItemID:      item,
		}
		l.deletes.sent(event.EventID, item)
		if err := s.ws.Send(event); err != nil {
			logger.Warn("OpenAI Realtime: failed to delete a blocked turn's item", "item_id", item, "error", err)
		}
	}
}

// deleteRejected reports whether a server error rejects a
// conversation.item.delete this session sent, logging it: the item was
// already gone, which is what the delete wanted.
func (s *RealtimeSession) deleteRejected(e *ErrorEvent) bool {
	if !s.turns.deletes.rejected(e.Error.EventID) {
		return false
	}
	logger.Debug("OpenAI Realtime: the item to delete was already gone",
		"code", e.Error.Code, "message", e.Error.Message)
	return true
}
