// Package core is the fixture's internal package.
package core

import (
	"context"
	"errors"

	"example.com/stage/internal/elem"
	"github.com/example/runtime/v2/types"
)

// Feed names a sibling internal package's types: stage exported Element
// before, so its wrapper names it as stage does; not Hidden.
func Feed(e *elem.Element, h elem.Hidden) []elem.Element { return nil }

// Kind is a kind.
type Kind int

// The kinds.
const (
	// KindA is the first.
	KindA Kind = iota
	KindB
	kindHidden
)

// Untyped is untyped.
const Untyped = 3

// Label is a lone typed const.
const Label Kind = 7

// Scored pairs an index with a score.
type Scored struct{ Index int }

// Reset is a method: aliases carry it.
func (s *Scored) Reset() {}

// NewScored returns a Scored.
func NewScored(i int) Scored { return Scored{Index: i} }

// Join is variadic.
func Join(parts ...string) string { return "" }

// Wrap has a blank and a versioned import.
func Wrap(ctx context.Context, _ int, msg *types.Message) (Scored, error) { return Scored{}, nil }

// Close has an unnamed parameter.
func Close(Scored) {}

// Helper is exported for stage's own use only.
func Helper() {}

// ErrGone is gone.
var ErrGone = errors.New("gone")

// Errors.
var (
	// ErrA is a.
	ErrA = errors.New("a")
	// ErrB is b.
	ErrB = errors.New("b")
	// ErrInternal is not stage's.
	ErrInternal = errors.New("internal")
)
