package core

import "example.com/stage/internal/elem"

// UseOther names elem.Other, but stage's Other aliases media.Other.
func UseOther(o *elem.Other) {}
