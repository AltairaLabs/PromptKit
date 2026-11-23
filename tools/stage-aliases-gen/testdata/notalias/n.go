package core

import "example.com/stage/internal/elem"

// UseScored names elem.Scored, but stage's Scored is its own type.
func UseScored(s *elem.Scored) {}
