package stage

import (
	"example.com/stage/internal/elem"
	"example.com/stage/internal/media"
)

type Kind int

const (
	KindA Kind = iota
	KindB
)

const Untyped = 3

const Label Kind = 7

type Scored struct{ Index int }

func NewScored(i int) Scored { return Scored{Index: i} }

func Join(parts ...string) string { return "" }

func Wrap(ctx context.Context, _ int, msg *types.Message) (Scored, error) { return Scored{}, nil }

func Close(Scored) {}

type Element = elem.Element

type Other = media.Other

func Feed(e *Element) []Element { return nil }

func UseScored(s *Scored) {}

func UseHidden(h elem.Hidden) {}

func UseOther(o *Other) {}

var ErrGone = errors.New("gone")

var (
	ErrA = errors.New("a")
	ErrB = errors.New("b")
)
