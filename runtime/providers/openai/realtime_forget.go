package openai

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// conversation.item.delete removes an item from the server's conversation;
// conversation.item.deleted acknowledges it.
const (
	realtimeEventItemDelete  = "conversation.item.delete"
	realtimeEventItemDeleted = "conversation.item.deleted"

	// errCodeItemNotFound is the error code the server answers a delete with
	// when it has no such item.
	errCodeItemNotFound = "item_delete_invalid_item_id"
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
	// genUser is the caller's item each response answers, by generation; a
	// response the session asked for itself (the spoken guardrail message)
	// answers none. genItems are the items each completed response output,
	// and genCalls whether it made tool calls. Kept for the responses the
	// session remembers (maxRespGens).
	genUser  map[int64]string
	genItems map[int64][]string
	genCalls map[int64]bool
	// pending holds the caller's forgotten items whose reply was still to
	// end when they were forgotten (in progress, not created yet, or a tool
	// round's follow-up still to come): each reply to them is deleted when it
	// ends.
	pending map[string]bool
	// deleted holds the items whose delete was sent, so none is sent twice.
	deleted map[string]bool
	// deletes maps each conversation.item.delete awaiting the server's answer
	// to its item, so a rejection that the item is gone is told apart from
	// any other error.
	deletes map[string]string
}

// ForgetTurn implements providers.TurnForgetter: it deletes, with
// conversation.item.delete, the caller's audio item inputID and the items of
// the response that answers it, so the model no longer has the turn in its
// context. The response that answers an item is one created while that item
// was the caller's latest, so a turn the caller started since is not touched.
//
// The caller's item is deleted at once. Its reply, when completed, is deleted
// at once too; one in progress, or not created yet, when its response.done
// arrives. When the reply completed with tool calls, the reply to their
// results is the one deleted, and the tool round is left: the conversation
// keeps each call with its result. The spoken guardrail message (Announce)
// is never deleted. An item is deleted once; a delete the server rejects
// because the item is gone is not a session error, and any other rejection
// is. A delete that could not be sent is reported and may be retried.
func (s *RealtimeSession) ForgetTurn(_ context.Context, inputID string) error {
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
	if inputID == "" {
		return nil
	}
	l := &s.turns
	err := s.deleteItems([]string{inputID})
	reply := l.replyTo(inputID)
	_, completed := l.genItems[reply]
	if reply == 0 || !completed || l.genCalls[reply] {
		if l.pending == nil {
			l.pending = map[string]bool{}
		}
		l.pending[inputID] = true
		return err
	}
	return errors.Join(err, s.deleteItems(l.genItems[reply]))
}

// replyTo is the generation of the latest response that answers the caller's
// item, or 0 when none was created yet.
func (l *turnLedger) replyTo(item string) int64 {
	var reply int64
	for gen, user := range l.genUser {
		if user == item && gen > reply {
			reply = gen
		}
	}
	return reply
}

// startTypedTurn records that the caller typed a turn (SendText): a response
// to it answers no audio item of theirs.
func (s *RealtimeSession) startTypedTurn() {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	s.turns.user = ""
}

// noteResponseStarted records the caller's item the response numbered gen
// answers, and forgets what the session knew of the response it no longer
// remembers (evicted). Called with recvMu held.
func (s *RealtimeSession) noteResponseStarted(gen, evicted int64) {
	l := &s.turns
	if l.genUser == nil {
		l.genUser = map[int64]string{}
		l.genItems = map[int64][]string{}
		l.genCalls = map[int64]bool{}
	}
	if evicted != 0 {
		delete(l.genUser, evicted)
		delete(l.genItems, evicted)
		delete(l.genCalls, evicted)
	}
	l.genUser[gen] = l.user
}

// noteAnnouncementStarted records that the response just created, when it is
// the spoken guardrail message (its request tag names an Announce, see
// realtime_response_queue.go), answers no caller input, so it is never
// deleted with one. Called with recvMu held, after responseCreated.
func (s *RealtimeSession) noteAnnouncementStarted() {
	if s.isAnnouncement(s.gen) {
		delete(s.turns.genUser, s.gen)
	}
}

// noteResponseDone records the items the response numbered gen output, and
// deletes them when the caller's item it answers was forgotten while it ran.
// Called with recvMu held.
func (s *RealtimeSession) noteResponseDone(gen int64, info *ResponseInfo) {
	if gen == 0 {
		return
	}
	l := &s.turns
	var items []string
	calls := false
	for i := range info.Output {
		items = append(items, info.Output[i].ID)
		calls = calls || info.Output[i].Type == typeFunctionCall
	}
	if l.genItems == nil {
		return
	}
	l.genItems[gen] = items
	l.genCalls[gen] = calls
	user, answers := l.genUser[gen]
	if !answers || user == "" || !l.pending[user] {
		return
	}
	if err := s.deleteItems(items); err != nil {
		logger.Warn("OpenAI Realtime: failed to delete a blocked turn's reply", "error", err)
	}
}

// deleteItems sends a delete for each item not yet deleted, in order, and
// marks it deleted once sent. Called with recvMu held.
func (s *RealtimeSession) deleteItems(items []string) error {
	l := &s.turns
	var errs []error
	for _, item := range items {
		if item == "" || l.deleted[item] {
			continue
		}
		event := conversationItemDeleteEvent{
			ClientEvent: ClientEvent{EventID: s.nextEventID(), Type: realtimeEventItemDelete},
			ItemID:      item,
		}
		if err := s.ws.Send(event); err != nil {
			errs = append(errs, fmt.Errorf("delete item %s: %w", item, err))
			continue
		}
		if l.deleted == nil {
			l.deleted = map[string]bool{}
			l.deletes = map[string]string{}
		}
		l.deleted[item] = true
		l.deletes[event.EventID] = item
	}
	return errors.Join(errs...)
}

// itemDeleted forgets the pending delete of item, which the server
// acknowledged. Called with recvMu held.
func (s *RealtimeSession) itemDeleted(item string) {
	for eventID, pending := range s.turns.deletes {
		if pending == item {
			delete(s.turns.deletes, eventID)
			return
		}
	}
}

// deleteRejected reports whether a server error only says that an item this
// session asked to delete was already gone, which is what the delete wanted:
// it is logged, not a session error. Any other rejection of a delete is.
func (s *RealtimeSession) deleteRejected(e *ErrorEvent) bool {
	item, ours := s.turns.deletes[e.Error.EventID]
	if !ours || e.Error.EventID == "" {
		return false
	}
	delete(s.turns.deletes, e.Error.EventID)
	if e.Error.Code != errCodeItemNotFound {
		logger.Warn("OpenAI Realtime: the server rejected deleting a blocked turn's item",
			"item_id", item, "code", e.Error.Code, "message", e.Error.Message)
		return false
	}
	logger.Debug("OpenAI Realtime: the item to delete was already gone", "item_id", item)
	return true
}
