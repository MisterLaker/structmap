package structmap_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/MisterLaker/structmap"
)

// codec converts the fixtures, which use the "tag" key.
var codec = structmap.Codec{Tag: "tag"}

type testSub1 struct {
	Type string `tag:"type,schema1,schema2,omitempty"`
	V1   string `tag:"v1,schema1,schema2,omitempty"`
	V2   string `tag:"v2,schema1"`
}

type testSub2 struct {
	Kind string `tag:"kind,schema1"`
	Up   bool   `tag:",omitempty"`
}

type testData struct {
	ID        string    `tag:"id,"`
	V1        string    `tag:"v1,schema1,schema2,omitempty"`
	V2        string    `tag:"v2,schema1,omitempty"`
	Sub1      *testSub1 `tag:"sub,schema1,omitempty"`
	Sub2      testSub2  `tag:",squash,schema1,schema2"`
	CreatedAt time.Time `tag:"created_at,schema1,omitzero"`
}

type testLeaves struct {
	Name       string              `tag:""`
	Ptr        *int                `tag:"ptr"`
	NilPtr     *int                `tag:"nil_ptr"`
	Bytes      []byte              `tag:"bytes"`
	Subs       []testSub1          `tag:"subs"`
	SubPtrs    []*testSub1         `tag:"sub_ptrs"`
	SubMap     map[string]testSub1 `tag:"sub_map"`
	Any        any                 `tag:"any"`
	Count      int                 `tag:"count,omitempty"`
	Skipped    string              `tag:"-"`
	Untagged   string
	unexported string `tag:"unexported"` //nolint:unused // verifies unexported fields are skipped
}

type testOverwrite struct {
	Type string   `tag:"type,omitempty"`
	V2   string   `tag:"v2"`
	Sub  testSub1 `tag:",squash,overwrite,schema1"`
}

type testStatus string

type testFlag bool

type testNumbers struct {
	I   int8       `tag:"i"`
	U   uint       `tag:"u"`
	F   float32    `tag:"f"`
	S   testStatus `tag:"s"`
	B   testFlag   `tag:"b"`
	Arr [2]int     `tag:"arr"`
}

// testKey is a map key encoded as text.
type testKey struct {
	A, B string
}

func (k testKey) MarshalText() ([]byte, error) {
	return []byte(k.A + ":" + k.B), nil
}

func (k *testKey) UnmarshalText(b []byte) error {
	a, rest, ok := strings.Cut(string(b), ":")
	if !ok {
		return errors.New("key: missing colon")
	}

	k.A, k.B = a, rest

	return nil
}

type testMapKeys struct {
	Ints   map[int]string     `tag:"ints"`
	Uints  map[uint8]string   `tag:"uints"`
	Texts  map[testKey]int    `tag:"texts"`
	Plain  map[string]int     `tag:"plain"`
	Floats map[float64]string `tag:"floats"`
}

// testMoney marshals to a scalar through a value receiver.
type testMoney int64

func (m testMoney) MarshalMap(structmap.Scope) (any, error) {
	return fmt.Sprintf("%d.%02d", m/100, m%100), nil
}

func (m *testMoney) UnmarshalMap(_ structmap.Scope, v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("money: unexpected %T", v)
	}

	var units, cents int64
	if _, err := fmt.Sscanf(s, "%d.%d", &units, &cents); err != nil {
		return fmt.Errorf("money: %w", err)
	}

	*m = testMoney(units*100 + cents)

	return nil
}

// testPoint marshals through a pointer receiver.
type testPoint struct {
	Lat, Lon float64
}

func (p *testPoint) MarshalMap(structmap.Scope) (any, error) {
	return []float64{p.Lat, p.Lon}, nil
}

// UnmarshalMap accepts its own []float64 output and its JSON round trip []any.
func (p *testPoint) UnmarshalMap(_ structmap.Scope, v any) error {
	var xs []float64

	switch t := v.(type) {
	case []float64:
		xs = t
	case []any:
		for _, x := range t {
			f, ok := x.(float64)
			if !ok {
				return fmt.Errorf("point: unexpected %T", x)
			}

			xs = append(xs, f)
		}
	}

	if len(xs) != 2 {
		return fmt.Errorf("point: want 2 coordinates, got %v", v)
	}

	p.Lat, p.Lon = xs[0], xs[1]

	return nil
}

// testProfile returns a schema dependent map.
type testProfile struct {
	Name, Email string
}

func (p testProfile) MarshalMap(s structmap.Scope) (any, error) {
	if s.Schema == "public" {
		return map[string]any{"name": p.Name}, nil
	}

	return map[string]any{"name": p.Name, "email": p.Email}, nil
}

// UnmarshalMap gets the parent map, testProfile is squashed.
func (p *testProfile) UnmarshalMap(_ structmap.Scope, v any) error {
	m, _ := v.(map[string]any)
	p.Name, _ = m["name"].(string)
	p.Email, _ = m["email"].(string)

	return nil
}

// testExtended extends the default output through a methodless copy.
type testExtended testSub1

func (e testExtended) MarshalMap(s structmap.Scope) (any, error) {
	type plain testExtended

	m, err := s.Codec().Marshal(s.Schema, plain(e))
	if err != nil {
		return nil, err
	}

	m["extended"] = true

	return m, nil
}

func (e *testExtended) UnmarshalMap(s structmap.Scope, v any) error {
	type plain testExtended

	m, _ := v.(map[string]any)
	if m["extended"] != true {
		return errors.New("extended: missing marker")
	}

	return s.Codec().Unmarshal(s.Schema, m, (*plain)(e))
}

// testFailing fails to marshal when Bad is set.
type testFailing struct {
	Bad bool
}

func (f testFailing) MarshalMap(structmap.Scope) (any, error) {
	if f.Bad {
		return nil, errors.New("failing: bad")
	}

	return "ok", nil
}

type testMarshalers struct {
	Price    testMoney            `tag:"price"`
	Prices   []testMoney          `tag:"prices"`
	PriceBy  map[string]testMoney `tag:"price_by"`
	Any      any                  `tag:"any"`
	Point    testPoint            `tag:"point"`
	PointPtr *testPoint           `tag:"point_ptr,omitempty"`
	Profile  testProfile          `tag:",squash"`
}

// testNode references itself to build cycles.
type testNode struct {
	Name string    `tag:"name"`
	Next *testNode `tag:"next,omitempty"`
}

func marshal(t *testing.T, schema string, v any) map[string]any {
	t.Helper()

	m, err := codec.Marshal(schema, v)
	require.NoError(t, err)

	return m
}
