package core

import "example.com/stage/internal/elem"

// UseHidden names elem.Hidden, which stage does not alias.
func UseHidden(h elem.Hidden) {}
