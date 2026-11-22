package history

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A block marks its canned reply on a copy of the reply's metadata, keeping
// what the metadata already held: a decision's map shared elsewhere is never
// written.
func TestMarkBlockedInput_MarksACopyKeepingTheMetadata(t *testing.T) {
	shared := map[string]interface{}{"validator": "topic_policy"}
	msg := types.Message{Role: "assistant", Content: "No.", Meta: shared}

	MarkBlockedInput(&msg)

	assert.Equal(t, map[string]interface{}{"validator": "topic_policy", types.MetaBlockedInput: true}, msg.Meta)
	assert.Equal(t, map[string]interface{}{"validator": "topic_policy"}, shared, "the shared map is not written")
	assert.True(t, IsMarkedBlockedInput(&msg))

	var bare types.Message
	MarkBlockedInput(&bare)
	assert.Equal(t, map[string]interface{}{types.MetaBlockedInput: true}, bare.Meta)
}

func TestWithBlockedInputMark(t *testing.T) {
	assert.Equal(t, map[string]interface{}{types.MetaBlockedInput: true}, WithBlockedInputMark(nil))
	in := map[string]interface{}{types.MetaBlockedInput: false, "k": 1}
	assert.Equal(t, map[string]interface{}{types.MetaBlockedInput: true, "k": 1}, WithBlockedInputMark(in))
	assert.Equal(t, map[string]interface{}{types.MetaBlockedInput: false, "k": 1}, in)
}

func TestMergeMeta(t *testing.T) {
	assert.Nil(t, MergeMeta(nil, nil))
	assert.Nil(t, MergeMeta(map[string]interface{}{}, map[string]interface{}{}))

	base := map[string]interface{}{"a": 1, "b": 2}
	add := map[string]interface{}{"b": 3, "c": 4}
	assert.Equal(t, map[string]interface{}{"a": 1, "b": 3, "c": 4}, MergeMeta(base, add), "add overlays base")
	assert.Equal(t, map[string]interface{}{"a": 1, "b": 2}, base, "base is not written")
	assert.Equal(t, map[string]interface{}{"b": 3, "c": 4}, add, "add is not written")

	assert.Equal(t, map[string]interface{}{"c": 4}, MergeMeta(nil, map[string]interface{}{"c": 4}))
	copied := MergeMeta(base, nil)
	assert.Equal(t, base, copied)
	copied["a"] = 9
	assert.Equal(t, 1, base["a"], "a merge of one map is a copy of it")
}

func TestIsBlockedRequestAnsweredBefore(t *testing.T) {
	marked := types.Message{Role: "user", Meta: map[string]interface{}{BlockedReplyBeforeMetaKey: true}}
	assert.True(t, IsBlockedRequestAnsweredBefore(&marked))
	notTrue := types.Message{Role: "user", Meta: map[string]interface{}{BlockedReplyBeforeMetaKey: "yes"}}
	assert.False(t, IsBlockedRequestAnsweredBefore(&notTrue))
	assert.False(t, IsBlockedRequestAnsweredBefore(&types.Message{Role: "user"}))
	assert.Equal(t, "_blocked_input_reply_before", BlockedReplyBeforeMetaKey, "the key is persisted, so it never changes")
}

// WithoutInterrupted takes the interrupted replies out and returns the
// blocked turns of what is left, aligned with it, found before the
// interrupted replies went.
func TestWithoutInterrupted(t *testing.T) {
	t.Run("aligns the blocked mask with what is kept", func(t *testing.T) {
		msgs := cutThenLateBlocked(types.Message{Role: "user", Content: "q2"})

		kept, blocked := WithoutInterrupted(msgs)

		assert.Equal(t, []string{"user:hello", "assistant:Hi!", "user:a wire transfer please", "user:q2"}, roleLines(kept))
		assert.Equal(t, []bool{false, false, true, false}, blocked,
			"the cut reply the blocked request took is gone; the answer before it is kept")
	})
	t.Run("without blocked turns the mask is nil", func(t *testing.T) {
		msgs := []types.Message{{Role: "user", Content: "q1"}, interruptedReply("half")}

		kept, blocked := WithoutInterrupted(msgs)

		assert.Equal(t, []string{"user:q1"}, roleLines(kept))
		assert.Nil(t, blocked)
	})
	t.Run("without interrupted replies the mask is the blocked turns", func(t *testing.T) {
		msgs := []types.Message{{Role: "user", Content: "forbidden"}, markedReply("refused"), {Role: "user", Content: "q2"}}

		kept, blocked := WithoutInterrupted(msgs)

		assert.Equal(t, msgs, kept)
		assert.Equal(t, []bool{true, true, false}, blocked)
	})
}

func TestWithoutMasked(t *testing.T) {
	msgs := []types.Message{{Role: "user", Content: "a"}, {Role: "user", Content: "b"}, {Role: "user", Content: "c"}}
	assert.Equal(t, []string{"user:a", "user:c"}, roleLines(WithoutMasked(msgs, []bool{false, true, false})))
	assert.Equal(t, msgs, WithoutMasked(msgs, nil))
}
