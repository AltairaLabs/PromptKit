package openai

// maxPendingTruncates bounds the truncate events awaiting the server's
// answer; past it the oldest is forgotten (its rejection, if one still
// came, would then surface as an ordinary server error).
const maxPendingTruncates = 32

// truncateTracker remembers the conversation.item.truncate events sent and
// not yet answered. An error names the event it rejects by its ID. The
// server's conversation.item.truncated does not echo the event ID, only the
// item, so it acknowledges the item's oldest pending truncate (the server
// answers an item's truncates in order). Receive goroutine only.
type truncateTracker struct {
	pending []pendingTruncate
}

type pendingTruncate struct {
	eventID, itemID string
}

// sent records a truncate event for itemID.
func (t *truncateTracker) sent(eventID, itemID string) {
	if len(t.pending) == maxPendingTruncates {
		t.pending = t.pending[1:]
	}
	t.pending = append(t.pending, pendingTruncate{eventID: eventID, itemID: itemID})
}

// done forgets itemID's oldest pending truncate, which the server
// acknowledged.
func (t *truncateTracker) done(itemID string) {
	t.remove(func(p pendingTruncate) bool { return p.itemID == itemID })
}

// rejected reports whether eventID is a pending truncate, forgetting it.
func (t *truncateTracker) rejected(eventID string) bool {
	return eventID != "" && t.remove(func(p pendingTruncate) bool { return p.eventID == eventID })
}

// remove drops the first pending truncate match accepts, and reports
// whether there was one.
func (t *truncateTracker) remove(match func(pendingTruncate) bool) bool {
	for i, p := range t.pending {
		if match(p) {
			t.pending = append(t.pending[:i], t.pending[i+1:]...)
			return true
		}
	}
	return false
}

// len is the number of truncates awaiting an answer.
func (t *truncateTracker) len() int { return len(t.pending) }
