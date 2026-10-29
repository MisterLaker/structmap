package structmap

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// diffCodec converts the fixtures, which use the "tag" key.
var diffCodec = Codec{Tag: "tag"}

type diffSub1 struct {
	Type string `tag:"type,schema1,schema2,omitempty"`
	V1   string `tag:"v1,schema1,schema2,omitempty"`
	V2   string `tag:"v2,schema1"`
}

type diffSub2 struct {
	Kind string `tag:"kind,schema1"`
	Up   bool   `tag:",omitempty"`
}

type diffData struct {
	ID        string    `tag:"id,"`
	V1        string    `tag:"v1,schema1,schema2,omitempty"`
	V2        string    `tag:"v2,schema1,omitempty"`
	Sub1      *diffSub1 `tag:"sub,schema1,omitempty"`
	Sub2      diffSub2  `tag:",squash,schema1,schema2"`
	CreatedAt time.Time `tag:"created_at,schema1,omitzero"`
}

type diffLeaves struct {
	Subs   []diffSub1          `tag:"subs"`
	SubMap map[string]diffSub1 `tag:"sub_map"`
	Bytes  []byte              `tag:"bytes"`
	Plain  map[string]int      `tag:"plain"`
	Arr    [2]diffSub1         `tag:"arr"`
}

// diffCollision squashes diffSub1 under keys the parent also sets.
type diffCollision struct {
	Type string   `tag:"type,omitempty"`
	V2   string   `tag:"v2"`
	Sub  diffSub1 `tag:",squash"`
}

type diffOverwrite struct {
	Type string   `tag:"type,omitempty"`
	V2   string   `tag:"v2"`
	Sub  diffSub1 `tag:",squash,overwrite,schema1"`
}

// diffMoney marshals to a scalar through a value receiver.
type diffMoney int64

func (m diffMoney) MarshalMap(Scope) (any, error) {
	return fmt.Sprintf("%d.%02d", m/100, m%100), nil
}

func (m *diffMoney) UnmarshalMap(_ Scope, v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("money: unexpected %T", v)
	}

	var units, cents int64
	if _, err := fmt.Sscanf(s, "%d.%d", &units, &cents); err != nil {
		return fmt.Errorf("money: %w", err)
	}

	*m = diffMoney(units*100 + cents)

	return nil
}

// diffPoint marshals through a pointer receiver.
type diffPoint struct {
	Lat, Lon float64
}

func (p *diffPoint) MarshalMap(Scope) (any, error) {
	return []float64{p.Lat, p.Lon}, nil
}

func (p *diffPoint) UnmarshalMap(_ Scope, v any) error {
	xs, ok := v.([]float64)
	if !ok || len(xs) != 2 {
		return fmt.Errorf("point: want 2 coordinates, got %v", v)
	}

	p.Lat, p.Lon = xs[0], xs[1]

	return nil
}

// diffProfile returns a schema dependent map and decodes from the parent map when squashed.
type diffProfile struct {
	Name, Email string
}

func (p diffProfile) MarshalMap(s Scope) (any, error) {
	if s.Schema == "public" {
		return map[string]any{"name": p.Name}, nil
	}

	return map[string]any{"name": p.Name, "email": p.Email}, nil
}

func (p *diffProfile) UnmarshalMap(_ Scope, v any) error {
	m, _ := v.(map[string]any)
	p.Name, _ = m["name"].(string)
	p.Email, _ = m["email"].(string)

	return nil
}

// diffFailing fails to marshal when Bad is set.
type diffFailing struct {
	Bad bool
}

func (f diffFailing) MarshalMap(Scope) (any, error) {
	if f.Bad {
		return nil, errors.New("failing: bad")
	}

	return "ok", nil
}

type diffMarshalers struct {
	Price   diffMoney   `tag:"price"`
	Prices  []diffMoney `tag:"prices"`
	Any     any         `tag:"any"`
	Point   diffPoint   `tag:"point"`
	Profile diffProfile `tag:",squash"`
}

// diffExtended extends the default output through a methodless copy.
type diffExtended diffSub1

func (e diffExtended) MarshalMap(s Scope) (any, error) {
	type plain diffExtended

	m, err := s.Codec().Marshal(plain(e), s.Schema)
	if err != nil {
		return nil, err
	}

	m["extended"] = true

	return m, nil
}

func (e *diffExtended) UnmarshalMap(s Scope, v any) error {
	type plain diffExtended

	m, _ := v.(map[string]any)
	if m["extended"] != true {
		return errors.New("extended: missing marker")
	}

	return s.Codec().Unmarshal(m, (*plain)(e), s.Schema)
}

// diffNode references itself to build cycles.
type diffNode struct {
	Name string    `tag:"name"`
	Next *diffNode `tag:"next,omitempty"`
}

func TestDiff(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	data := func() diffData {
		return diffData{
			ID:        "1",
			V1:        "a",
			V2:        "b",
			Sub1:      &diffSub1{Type: "x", V1: "w", V2: "y"},
			Sub2:      diffSub2{Kind: "k", Up: true},
			CreatedAt: ts,
		}
	}

	tests := []struct {
		name   string
		schema string
		change func(*diffData)
		want   map[string]any
		equal  bool // b only differs by values Equal reports equal, so a patched a keeps them
	}{
		{
			name:   "equal",
			change: func(*diffData) {},
			want:   nil,
		},
		{
			name:   "scalar",
			change: func(d *diffData) { d.ID = "2" },
			want:   map[string]any{"id": "2"},
		},
		{
			name:   "omitempty removed",
			change: func(d *diffData) { d.V2 = "" },
			want:   map[string]any{"v2": nil},
		},
		{
			name:   "omitzero removed",
			change: func(d *diffData) { d.CreatedAt = time.Time{} },
			want:   map[string]any{"created_at": nil},
		},
		{
			name:   "nested partial",
			change: func(d *diffData) { d.Sub1.V2 = "z" },
			want:   map[string]any{"sub": map[string]any{"v2": "z"}},
		},
		{
			name:   "nested removed",
			change: func(d *diffData) { d.Sub1 = nil },
			want:   map[string]any{"sub": nil},
		},
		{
			name:   "squash",
			change: func(d *diffData) { d.Sub2.Kind = "q" },
			want:   map[string]any{"kind": "q"},
		},
		{
			name:   "squash removed",
			change: func(d *diffData) { d.Sub2.Up = false },
			want:   map[string]any{"Up": nil},
		},
		{
			name:   "time location ignored",
			change: func(d *diffData) { d.CreatedAt = ts.In(time.FixedZone("UTC+2", 2*60*60)) },
			want:   nil,
			equal:  true,
		},
		{
			name:   "time changed",
			change: func(d *diffData) { d.CreatedAt = ts.Add(time.Hour) },
			want:   map[string]any{"created_at": ts.Add(time.Hour)},
		},
		{
			name:   "outside schema",
			schema: "schema2",
			change: func(d *diffData) { d.V2, d.Sub1.V2, d.Sub2.Kind = "c", "z", "q" },
			want:   nil,
		},
		{
			name:   "in schema",
			schema: "schema2",
			change: func(d *diffData) { d.V1, d.Sub2.Up = "c", false },
			want:   map[string]any{"v1": "c", "Up": nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, b := data(), data()
			tt.change(&b)

			got, err := diffCodec.Diff(a, b, tt.schema)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			if !tt.equal {
				for _, schema := range []string{"", "schema1", "schema2", "other"} {
					assertPatch(t, schema, data, b)
				}
			}
		})
	}

	t.Run("nested added", func(t *testing.T) {
		t.Parallel()

		got, err := diffCodec.Diff(diffData{}, diffData{Sub1: &diffSub1{V2: "y"}}, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"sub": map[string]any{"v2": "y"}}, got)
	})

	t.Run("leaves replaced whole", func(t *testing.T) {
		t.Parallel()

		a := func() diffLeaves {
			return diffLeaves{
				Subs:   []diffSub1{{Type: "a"}, {Type: "b"}},
				SubMap: map[string]diffSub1{"x": {Type: "a"}, "y": {Type: "b"}},
				Bytes:  []byte("ab"),
				Plain:  map[string]int{"a": 1, "b": 2},
				Arr:    [2]diffSub1{{Type: "a"}, {Type: "b"}},
			}
		}

		b := a()
		b.Subs = []diffSub1{{Type: "a"}, {Type: "c"}}
		b.SubMap = map[string]diffSub1{"x": {Type: "a"}, "y": {Type: "c"}}
		b.Bytes = []byte("ac")
		b.Plain = map[string]int{"a": 1, "b": 3}
		b.Arr[1].Type = "c"

		got, err := diffCodec.Diff(a(), b, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"subs":    []any{map[string]any{"type": "a", "v2": ""}, map[string]any{"type": "c", "v2": ""}},
			"sub_map": map[string]any{"x": map[string]any{"type": "a", "v2": ""}, "y": map[string]any{"type": "c", "v2": ""}},
			"bytes":   []byte("ac"),
			"plain":   map[string]int{"a": 1, "b": 3},
			"arr":     []any{map[string]any{"type": "a", "v2": ""}, map[string]any{"type": "c", "v2": ""}},
		}, got)

		assertPatch(t, "", a, b)
	})

	t.Run("nil and empty differ", func(t *testing.T) {
		t.Parallel()

		got, err := diffCodec.Diff(diffLeaves{}, diffLeaves{Subs: []diffSub1{}}, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"subs": []any{}}, got)
	})

	t.Run("squash collision", func(t *testing.T) {
		t.Parallel()

		a := func() diffCollision {
			return diffCollision{Type: "p", V2: "p", Sub: diffSub1{Type: "s", V1: "s", V2: "s"}}
		}

		hidden := a()
		hidden.Sub.Type, hidden.Sub.V2 = "t", "t"

		got, err := diffCodec.Diff(a(), hidden, "")
		require.NoError(t, err)
		assert.Nil(t, got, "keys the parent sets hide the squashed ones")

		uncovered := a()
		uncovered.Type = ""

		got, err = diffCodec.Diff(a(), uncovered, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"type": "s"}, got)

		assertPatch(t, "", a, hidden)
		assertPatch(t, "", a, uncovered)
	})

	t.Run("squash pointer", func(t *testing.T) {
		t.Parallel()

		type squashPtr struct {
			ID  string    `tag:"id"`
			Sub *diffSub1 `tag:",squash"`
		}

		a := func() squashPtr { return squashPtr{ID: "1", Sub: &diffSub1{Type: "x", V2: "y"}} }

		changed := a()
		changed.Sub = &diffSub1{Type: "x", V2: "z"}

		got, err := diffCodec.Diff(a(), changed, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"v2": "z"}, got)

		got, err = diffCodec.Diff(a(), squashPtr{ID: "1"}, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"type": nil, "v2": nil}, got)

		assertPatch(t, "", a, changed)
		assertPatch(t, "", func() squashPtr { return squashPtr{ID: "1"} }, a())
	})

	t.Run("squash overwrite", func(t *testing.T) {
		t.Parallel()

		a := func() diffOverwrite {
			return diffOverwrite{Type: "p", V2: "p", Sub: diffSub1{Type: "s", V2: "s"}}
		}

		b := a()
		b.V2, b.Sub.V2 = "q", "t"

		got, err := diffCodec.Diff(a(), b, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"v2": "t"}, got)

		assertPatch(t, "", a, b)
	})

	t.Run("marshalers", func(t *testing.T) {
		t.Parallel()

		a := func() diffMarshalers {
			return diffMarshalers{
				Price:   1250,
				Prices:  []diffMoney{1, 2},
				Point:   diffPoint{Lat: 1, Lon: 2},
				Profile: diffProfile{Name: "n", Email: "e"},
			}
		}

		b := a()
		b.Price = 1300
		b.Point.Lat = 3
		b.Profile.Name = "m"

		got, err := diffCodec.Diff(a(), b, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"price": "13.00",
			"point": []float64{3, 2},
			"name":  "m",
			"email": "e", // a squashed Unmarshaler gets all of its keys
		}, got)

		assertPatch(t, "", a, b)

		got, err = diffCodec.Diff(a(), b, "public")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"price": "13.00", "point": []float64{3, 2}, "name": "m"}, got)
	})

	t.Run("root marshaler replaced whole", func(t *testing.T) {
		t.Parallel()

		a := func() diffExtended { return diffExtended{Type: "x", V2: "y"} }
		b := diffExtended{Type: "z", V2: "y"}

		got, err := diffCodec.Diff(a(), b, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"type": "z", "v2": "y", "extended": true}, got)

		got, err = diffCodec.Diff(a(), a(), "")
		require.NoError(t, err)
		assert.Nil(t, got)

		assertPatch(t, "", a, b)
	})

	t.Run("nil roots", func(t *testing.T) {
		t.Parallel()

		b := &diffSub1{Type: "x", V2: "y"}

		got, err := diffCodec.Diff(nil, b, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"type": "x", "v2": "y"}, got)

		got, err = diffCodec.Diff(b, nil, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"type": nil, "v2": nil}, got)

		got, err = diffCodec.Diff[*diffSub1](nil, nil, "")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("pointer roots", func(t *testing.T) {
		t.Parallel()

		got, err := diffCodec.Diff(&diffSub1{V2: "a"}, &diffSub1{V2: "b"}, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"v2": "b"}, got)
	})

	t.Run("interface roots", func(t *testing.T) {
		t.Parallel()

		got, err := diffCodec.Diff[any](diffSub1{V2: "a"}, &diffSub1{V2: "b"}, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"v2": "b"}, got)

		got, err = diffCodec.Diff[any](diffSub1{V2: "a"}, diffSub2{Kind: "k"}, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"v2": nil, "kind": "k"}, got)
	})

	t.Run("cycle", func(t *testing.T) {
		t.Parallel()

		cycle := func() *diffNode {
			n := &diffNode{Name: "a"}
			n.Next = n

			return n
		}

		_, err := diffCodec.Diff(cycle(), cycle(), "")
		require.ErrorIs(t, err, ErrCycle)
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()

		type failing struct {
			Sub *struct {
				Any any `tag:"any"`
			} `tag:"sub"`
		}

		a := failing{Sub: &struct {
			Any any `tag:"any"`
		}{}}
		b := failing{Sub: &struct {
			Any any `tag:"any"`
		}{Any: diffFailing{Bad: true}}}

		_, err := diffCodec.Diff(a, b, "")

		var fe *FieldError
		require.ErrorAs(t, err, &fe)
		assert.Equal(t, "sub.any", fe.Path)

		_, err = diffCodec.Diff(diffMarshalers{Any: diffFailing{Bad: true}}, diffMarshalers{}, "")
		require.ErrorAs(t, err, &fe)
		assert.Equal(t, "any", fe.Path)
		assert.EqualError(t, errors.Unwrap(err), "failing: bad")
	})

	t.Run("not a struct", func(t *testing.T) {
		t.Parallel()

		assert.Panics(t, func() { _, _ = diffCodec.Diff(1, 2, "") })
		assert.PanicsWithValue(t, "structmap: empty tag", func() { _, _ = Codec{}.Diff(diffData{}, diffData{}, "") })
	})
}

// shallowCodec is diffCodec with Shallow.
var shallowCodec = Codec{Tag: "tag", Shallow: true}

func TestDiffShallow(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	data := func() diffData {
		return diffData{
			ID:        "1",
			V1:        "a",
			V2:        "b",
			Sub1:      &diffSub1{Type: "x", V1: "w", V2: "y"},
			Sub2:      diffSub2{Kind: "k", Up: true},
			CreatedAt: ts,
		}
	}

	tests := []struct {
		name   string
		schema string
		change func(*diffData)
		want   map[string]any
	}{
		{
			name:   "equal",
			change: func(*diffData) {},
			want:   nil,
		},
		{
			name:   "scalar",
			change: func(d *diffData) { d.ID = "2" },
			want:   map[string]any{"id": "2"},
		},
		{
			name:   "nested whole",
			change: func(d *diffData) { d.Sub1.V2 = "z" },
			want:   map[string]any{"sub": map[string]any{"type": "x", "v1": "w", "v2": "z"}},
		},
		{
			name:   "nested key omitted",
			change: func(d *diffData) { d.Sub1.V1 = "" },
			want:   map[string]any{"sub": map[string]any{"type": "x", "v2": "y"}},
		},
		{
			name:   "nested outside schema",
			schema: "schema2",
			change: func(d *diffData) { d.Sub1.V2 = "z" },
			want:   nil,
		},
		{
			name:   "nested in schema",
			schema: "schema1",
			change: func(d *diffData) { d.Sub1.Type = "q" },
			want:   map[string]any{"sub": map[string]any{"type": "q", "v1": "w", "v2": "y"}},
		},
		{
			name:   "nested removed",
			change: func(d *diffData) { d.Sub1 = nil },
			want:   map[string]any{"sub": nil},
		},
		{
			name:   "squash",
			change: func(d *diffData) { d.Sub2.Kind = "q" },
			want:   map[string]any{"kind": "q"},
		},
		{
			name:   "squash removed",
			change: func(d *diffData) { d.Sub2.Up = false },
			want:   map[string]any{"Up": nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, b := data(), data()
			tt.change(&b)

			got, err := shallowCodec.Diff(a, b, tt.schema)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			for _, schema := range []string{"", "schema1", "schema2", "other"} {
				assertReplacePatch(t, schema, data(), b)
			}
		})
	}

	t.Run("nested added", func(t *testing.T) {
		t.Parallel()

		b := diffData{Sub1: &diffSub1{V2: "y"}}

		got, err := shallowCodec.Diff(diffData{}, b, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"sub": map[string]any{"v2": "y"}}, got)

		assertReplacePatch(t, "", diffData{}, b)
	})

	t.Run("nested values", func(t *testing.T) {
		t.Parallel()

		type nested struct {
			Sub diffSub1 `tag:"sub"`
		}

		a, b := nested{Sub: diffSub1{Type: "x", V2: "y"}}, nested{Sub: diffSub1{Type: "x", V2: "z"}}

		got, err := shallowCodec.Diff(a, b, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"sub": map[string]any{"type": "x", "v2": "z"}}, got)

		got, err = shallowCodec.Diff(a, a, "")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("roots still diffed", func(t *testing.T) {
		t.Parallel()

		got, err := shallowCodec.Diff(&diffSub1{V2: "a"}, &diffSub1{V2: "b"}, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"v2": "b"}, got)
	})

	t.Run("squash collision", func(t *testing.T) {
		t.Parallel()

		a := diffCollision{Type: "p", V2: "p", Sub: diffSub1{Type: "s", V1: "s", V2: "s"}}
		b := a
		b.Sub.V1 = "t"

		got, err := shallowCodec.Diff(a, b, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"v1": "t"}, got)

		assertReplacePatch(t, "", a, b)
	})
}

// assertReplacePatch checks that replacing a's top-level keys with the Shallow diff from a to b,
// as a store that replaces top-level values applies it, gives what b marshals to.
// nil values are dropped from both sides: a store either keeps them as null or removes the key.
func assertReplacePatch[T any](t *testing.T, schema string, a, b T) {
	t.Helper()

	patch, err := shallowCodec.Diff(a, b, schema)
	require.NoError(t, err)

	got, err := shallowCodec.Marshal(a, schema)
	require.NoError(t, err)

	if got == nil {
		got = make(map[string]any)
	}

	maps.Copy(got, patch)

	want, err := shallowCodec.Marshal(b, schema)
	require.NoError(t, err)

	assert.Equal(t, dropNil(want), dropNil(got))
}

func dropNil(m map[string]any) map[string]any {
	maps.DeleteFunc(m, func(_ string, v any) bool { return v == nil })

	return m
}

// assertPatch checks that the diff from a to b, unmarshaled into a, gives a value that marshals as b.
// a returns a new value each time, so the patch does not modify the pointers it shares with b.
func assertPatch[T any](t *testing.T, schema string, a func() T, b T) {
	t.Helper()

	patch, err := diffCodec.Diff(a(), b, schema)
	require.NoError(t, err)

	got := a()
	require.NoError(t, diffCodec.Unmarshal(patch, &got, schema))

	want, err := diffCodec.Marshal(b, schema)
	require.NoError(t, err)

	gotMap, err := diffCodec.Marshal(got, schema)
	require.NoError(t, err)

	assert.Equal(t, want, gotMap)
}

type sameItem struct {
	Name string   `tag:"name"`
	Qty  int      `tag:"qty,omitempty"`
	Tags []string `tag:"tags,omitempty"`
	Note string   `tag:"note,private"`
}

type sameSquash struct {
	ID   string   `tag:"id"`
	Item sameItem `tag:",squash"`
}

type sameText string

// sameKey is a map key Marshal formats with MarshalText.
type sameKey struct {
	S string
}

func (k sameKey) MarshalText() ([]byte, error) {
	if k.S == "bad" {
		return nil, errors.New("text: bad")
	}

	return []byte(k.S), nil
}

type sameMoney int

func (m sameMoney) MarshalMap(Scope) (any, error) {
	return int(m) / 100, nil
}

// sameVersion is equal by major version only.
type sameVersion struct {
	Major, Minor int
}

func (v sameVersion) Equal(o sameVersion) bool {
	return v.Major == o.Major
}

type sameUntagged struct {
	A int
	b string
}

type samePair struct {
	name string
	x, y reflect.Value
	fast bool // same can tell without marshaling
}

// pair returns addressable values of the static type T, as fields of the values passed to Diff are.
func pair[T any](name string, fast bool, x, y T) samePair {
	return samePair{name: name, x: reflect.ValueOf(&x).Elem(), y: reflect.ValueOf(&y).Elem(), fast: fast}
}

// TestSame checks that same agrees with comparing the Marshal outputs, which it replaces.
func TestSame(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	shared := []sameItem{{Name: "a"}}
	sharedMap := map[string]sameItem{"a": {Name: "a"}}
	item := &sameItem{Name: "a"}

	pairs := []samePair{
		pair("bool", true, true, false),
		pair("int", true, 1, 1),
		pair("int8", true, int8(1), int8(2)),
		pair("uint", true, uint(1), uint(2)),
		pair("float", true, 1.5, 1.5),
		pair("nan", true, math.NaN(), math.NaN()),
		pair("zero signs", true, 0.0, math.Copysign(0, -1)),
		pair("complex", true, 1+2i, 1+3i),
		pair("string", true, "a", "b"),
		pair("named", true, sameText("a"), sameText("a")),
		pair("time same instant", true, ts, ts.In(time.FixedZone("UTC+2", 2*60*60))),
		pair("time differ", true, ts, ts.Add(time.Second)),
		pair("equal method", true, sameVersion{1, 2}, sameVersion{1, 3}),
		pair("equal method differ", true, sameVersion{1, 2}, sameVersion{2, 2}),
		pair("untagged struct", true, sameUntagged{1, "a"}, sameUntagged{1, "b"}),
		pair("bytes", true, []byte("ab"), []byte("ab")),
		pair("strings nil empty", true, []string(nil), []string{}),
		pair("strings", true, []string{"a"}, []string{"b"}),
		pair("times kept as is", true, []time.Time{ts}, []time.Time{ts.In(time.FixedZone("UTC+2", 2*60*60))}),
		pair("plain map", true, map[string]int{"a": 1}, map[string]int{"a": 1}),
		pair("plain map differ", true, map[string]int{"a": 1}, map[string]int{"a": 2}),
		pair("array", true, [2]sameItem{{Name: "a"}}, [2]sameItem{{Name: "a"}}),
		pair("array differ", true, [2]sameItem{{Name: "a"}}, [2]sameItem{{}, {Name: "a"}}),
		pair("struct", true, sameItem{Name: "a", Qty: 1}, sameItem{Name: "a", Qty: 1}),
		pair("struct omitted", true, sameItem{Name: "a"}, sameItem{Name: "a", Qty: 1}),
		pair("struct empty slice omitted", true, sameItem{Tags: []string{}}, sameItem{}),
		pair("struct outside schema", true, sameItem{Note: "a"}, sameItem{Note: "b"}),
		pair("struct squash", false, sameSquash{ID: "1"}, sameSquash{ID: "1"}),
		pair("structs", true, []sameItem{{Name: "a"}, {Name: "b"}}, []sameItem{{Name: "a"}, {Name: "b"}}),
		pair("structs differ", true, []sameItem{{Name: "a"}}, []sameItem{{Name: "a", Tags: []string{"x"}}}),
		pair("structs length", true, []sameItem{{Name: "a"}}, []sameItem{{Name: "a"}, {}}),
		pair("structs nil empty", true, []sameItem(nil), []sameItem{}),
		pair("structs shared", true, shared, shared),
		pair("squashes", true, []sameSquash{{ID: "1"}}, []sameSquash{{ID: "2"}}),
		pair("struct map", true, map[string]sameItem{"a": {Name: "a"}}, map[string]sameItem{"a": {Name: "a"}}),
		pair("struct map differ", true, map[string]sameItem{"a": {Name: "a"}}, map[string]sameItem{"a": {Name: "b"}}),
		pair("struct map keys", true, map[string]sameItem{"a": {}}, map[string]sameItem{"b": {}}),
		pair("struct map nil", true, map[string]sameItem(nil), map[string]sameItem{}),
		pair("struct map shared", true, sharedMap, sharedMap),
		pair("int keys", true, map[int]sameItem{1: {Name: "a"}}, map[int]sameItem{1: {Name: "a"}}),
		pair("string kind text keys", true, map[sameText]sameItem{"a": {}}, map[sameText]sameItem{"a": {}}),
		pair("text keys", false, map[sameKey]sameItem{{"a"}: {}}, map[sameKey]sameItem{{"a"}: {}}),
		pair("pointer", true, &sameItem{Name: "a"}, &sameItem{Name: "a"}),
		pair("pointer differ", true, &sameItem{Name: "a"}, &sameItem{Name: "b"}),
		pair("pointer shared", true, item, item),
		pair("pointers nil", true, (*sameItem)(nil), (*sameItem)(nil)),
		pair("pointer nil", false, (*sameItem)(nil), &sameItem{}),
		pair("interface", true, any(sameItem{Name: "a"}), any(sameItem{Name: "a"})),
		pair("interface types", false, any(sameItem{Name: "a"}), any(&sameItem{Name: "a"})),
		pair("interface ints", false, any(1), any(int64(1))),
		pair("interfaces nil", true, any(nil), any(nil)),
		pair("marshaler", false, sameMoney(100), sameMoney(150)),
		pair("marshalers", true, []sameMoney{100}, []sameMoney{150}),
		pair("marshalers differ", true, []sameMoney{100}, []sameMoney{250}),
	}

	for _, p := range pairs {
		for _, schema := range []string{"", "public"} {
			for _, addressable := range []bool{true, false} {
				x, y := p.x, p.y
				if !addressable {
					x, y = reflect.ValueOf(x.Interface()), reflect.ValueOf(y.Interface())
				}

				d := diffState{enc: encodeState{scope: diffCodec.scope(schema), cache: diffCodec.cache()}}

				msg := fmt.Sprintf("%s, schema %q, addressable %v", p.name, schema, addressable)

				outX, err := d.enc.value(x)
				require.NoError(t, err, msg)

				outY, err := d.enc.value(y)
				require.NoError(t, err, msg)

				want := d.equal(outX, outY)

				got, ok, err := d.same(x, y)
				require.NoError(t, err, msg)

				if addressable && x.Kind() != reflect.Interface {
					assert.Equal(t, p.fast, ok, "%s: same can tell", msg)
				}

				if ok {
					assert.Equal(t, want, got, "%s: same agrees with the Marshal outputs", msg)
				}

				got, err = d.sameValue(x, y)
				require.NoError(t, err, msg)
				assert.Equal(t, want, got, "%s: sameValue agrees with the Marshal outputs", msg)
			}
		}
	}
}

func TestSameErrors(t *testing.T) {
	t.Parallel()

	d := diffState{enc: encodeState{scope: diffCodec.scope(""), cache: diffCodec.cache()}}

	x := map[string]map[sameKey]sameItem{"k": {{"bad"}: {}}}
	y := map[string]map[sameKey]sameItem{"k": {{"bad"}: {}}}

	_, err := d.sameValue(reflect.ValueOf(x), reflect.ValueOf(y))

	var fe *FieldError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, "[k]", fe.Path)

	type node struct {
		Next *node `tag:"next"`
	}

	cycle := func() *node {
		n := &node{}
		n.Next = n

		return n
	}

	_, err = d.sameValue(reflect.ValueOf(cycle()), reflect.ValueOf(cycle()))
	require.ErrorIs(t, err, ErrCycle)
}
