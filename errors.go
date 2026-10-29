package structmap

import (
	"errors"
	"strconv"
	"strings"
)

// Sentinel errors wrapped by conversion errors, match them with errors.Is.
var (
	ErrType     = errors.New("type mismatch")
	ErrOverflow = errors.New("overflow")
	ErrFraction = errors.New("not an integer")
	ErrNegative = errors.New("negative")
	ErrLength   = errors.New("length mismatch")
	ErrCycle    = errors.New("cycle")
)

// FieldError is a conversion error with the path of the field it happened at,
// such as "sub.items[2].name" or "prices[eur]".
type FieldError struct {
	Path string
	Err  error
}

func (e *FieldError) Error() string {
	return e.Path + ": " + e.Err.Error()
}

func (e *FieldError) Unwrap() error {
	return e.Err
}

type segmentKind uint8

const (
	segmentField segmentKind = iota
	segmentIndex
	segmentKey
)

type segment struct {
	kind  segmentKind
	name  string
	index int
}

// path is the stack of segments to the value being converted, it is formatted only on error.
type path []segment

func (p path) String() string {
	var b strings.Builder

	for i, s := range p {
		switch s.kind {
		case segmentField:
			if i > 0 {
				b.WriteByte('.')
			}

			b.WriteString(s.name)
		case segmentIndex:
			b.WriteByte('[')
			b.WriteString(strconv.Itoa(s.index))
			b.WriteByte(']')
		case segmentKey:
			b.WriteByte('[')
			b.WriteString(s.name)
			b.WriteByte(']')
		}
	}

	return b.String()
}

// wrap attaches the path to err, errors at the root are returned as is.
func (p path) wrap(err error) error {
	if err == nil || len(p) == 0 {
		return err
	}

	return &FieldError{Path: p.String(), Err: err}
}
