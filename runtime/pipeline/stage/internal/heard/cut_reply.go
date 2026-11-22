package heard

import (
	"math"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// cutReplyMetaKeys is how many keys CutReply adds to a Message's Meta.
const cutReplyMetaKeys = 4

// playedFractionScale rounds the played fraction to three decimal places.
const playedFractionScale = 1000

// CutReply is a copy of msg recording that heard of its total audio was
// played. The text and FinishReason are kept.
func CutReply(msg *types.Message, heard, total time.Duration, cause string) *types.Message {
	c := *msg
	c.Meta = make(map[string]interface{}, len(msg.Meta)+cutReplyMetaKeys)
	for k, v := range msg.Meta {
		c.Meta[k] = v
	}
	c.Meta[types.MetaPlayoutCut] = cause
	c.Meta[types.MetaHeardMs] = heard.Milliseconds()
	c.Meta[types.MetaAudioMs] = total.Milliseconds()
	fraction := 0.0
	if total > 0 {
		fraction = math.Round(float64(heard)/float64(total)*playedFractionScale) / playedFractionScale
	}
	c.Meta[types.MetaPlayedFraction] = fraction
	return &c
}
