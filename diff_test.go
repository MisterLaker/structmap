package structmap_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MisterLaker/structmap"
)

// testCollision squashes testSub1 under keys the parent also sets.
type testCollision struct {
	Type string   `tag:"type,omitempty"`
	V2   string   `tag:"v2"`
	Sub  testSub1 `tag:",squash"`
}

type testDiffLeaves struct {
	Leaves testLeaves  `tag:"leaves"`
	Keys   testMapKeys `tag:",squash"`
	Arr    [2]testSub1 `tag:"arr"`
}

func TestDiff(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	data := func() testData {
		return testData{
			ID:        "1",
			V1:        "a",
			V2:        "b",
			Sub1:      &testSub1{Type: "x", V1: "w", V2: "y"},
			Sub2:      testSub2{Kind: "k", Up: true},
			CreatedAt: ts,
		}
	}

	tests := []struct {
		name   string
		schema string
		change func(*testData)
		want   map[string]any
		equal  bool // b only differs by values Equal reports equal, so a patched a keeps them
	}{
		{
			name:   "equal",
			change: func(*testData) {},
			want:   nil,
		},
		{
			name:   "scalar",
			change: func(d *testData) { d.ID = "2" },
			want:   map[string]any{"id": "2"},
		},
		{
			name:   "omitempty removed",
			change: func(d *testData) { d.V2 = "" },
			want:   map[string]any{"v2": nil},
		},
		{
			name:   "omitzero removed",
			change: func(d *testData) { d.CreatedAt = time.Time{} },
			want:   map[string]any{"created_at": nil},
		},
		{
			name:   "nested partial",
			change: func(d *testData) { d.Sub1.V2 = "z" },
			want:   map[string]any{"sub": map[string]any{"v2": "z"}},
		},
		{
			name:   "nested removed",
			change: func(d *testData) { d.Sub1 = nil },
			want:   map[string]any{"sub": nil},
		},
		{
			name:   "squash",
			change: func(d *testData) { d.Sub2.Kind = "q" },
			want:   map[string]any{"kind": "q"},
		},
		{
			name:   "squash removed",
			change: func(d *testData) { d.Sub2.Up = false },
			want:   map[string]any{"Up": nil},
		},
		{
			name:   "time location ignored",
			change: func(d *testData) { d.CreatedAt = ts.In(time.FixedZone("UTC+2", 2*60*60)) },
			want:   nil,
			equal:  true,
		},
		{
			name:   "time changed",
			change: func(d *testData) { d.CreatedAt = ts.Add(time.Hour) },
			want:   map[string]any{"created_at": ts.Add(time.Hour)},
		},
		{
			name:   "outside schema",
			schema: "schema2",
			change: func(d *testData) { d.V2, d.Sub1.V2, d.Sub2.Kind = "c", "z", "q" },
			want:   nil,
		},
		{
			name:   "in schema",
			schema: "schema2",
			change: func(d *testData) { d.V1, d.Sub2.Up = "c", false },
			want:   map[string]any{"v1": "c", "Up": nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, b := data(), data()
			tt.change(&b)

			got, err := codec.Diff(tt.schema, a, b)
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

		got, err := codec.Diff("", testData{}, testData{Sub1: &testSub1{V2: "y"}})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"sub": map[string]any{"v2": "y"}}, got)
	})

	t.Run("leaves replaced whole", func(t *testing.T) {
		t.Parallel()

		a := func() testDiffLeaves {
			return testDiffLeaves{
				Leaves: testLeaves{
					Subs:   []testSub1{{Type: "a"}, {Type: "b"}},
					SubMap: map[string]testSub1{"x": {Type: "a"}, "y": {Type: "b"}},
					Bytes:  []byte("ab"),
				},
				Keys: testMapKeys{Plain: map[string]int{"a": 1, "b": 2}},
				Arr:  [2]testSub1{{Type: "a"}, {Type: "b"}},
			}
		}

		b := a()
		b.Leaves.Subs = []testSub1{{Type: "a"}, {Type: "c"}}
		b.Leaves.SubMap = map[string]testSub1{"x": {Type: "a"}, "y": {Type: "c"}}
		b.Leaves.Bytes = []byte("ac")
		b.Keys.Plain = map[string]int{"a": 1, "b": 3}
		b.Arr[1].Type = "c"

		got, err := codec.Diff("", a(), b)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"leaves": map[string]any{
				"subs":    []any{map[string]any{"type": "a", "v2": ""}, map[string]any{"type": "c", "v2": ""}},
				"sub_map": map[string]any{"x": map[string]any{"type": "a", "v2": ""}, "y": map[string]any{"type": "c", "v2": ""}},
				"bytes":   []byte("ac"),
			},
			"plain": map[string]int{"a": 1, "b": 3},
			"arr":   []any{map[string]any{"type": "a", "v2": ""}, map[string]any{"type": "c", "v2": ""}},
		}, got)

		assertPatch(t, "", a, b)
	})

	t.Run("nil and empty differ", func(t *testing.T) {
		t.Parallel()

		got, err := codec.Diff("", testLeaves{}, testLeaves{Subs: []testSub1{}})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"subs": []any{}}, got)
	})

	t.Run("squash collision", func(t *testing.T) {
		t.Parallel()

		a := func() testCollision {
			return testCollision{Type: "p", V2: "p", Sub: testSub1{Type: "s", V1: "s", V2: "s"}}
		}

		hidden := a()
		hidden.Sub.Type, hidden.Sub.V2 = "t", "t"

		got, err := codec.Diff("", a(), hidden)
		require.NoError(t, err)
		assert.Nil(t, got, "keys the parent sets hide the squashed ones")

		uncovered := a()
		uncovered.Type = ""

		got, err = codec.Diff("", a(), uncovered)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"type": "s"}, got)

		assertPatch(t, "", a, hidden)
		assertPatch(t, "", a, uncovered)
	})

	t.Run("squash pointer", func(t *testing.T) {
		t.Parallel()

		type squashPtr struct {
			ID  string    `tag:"id"`
			Sub *testSub1 `tag:",squash"`
		}

		a := func() squashPtr { return squashPtr{ID: "1", Sub: &testSub1{Type: "x", V2: "y"}} }

		changed := a()
		changed.Sub = &testSub1{Type: "x", V2: "z"}

		got, err := codec.Diff("", a(), changed)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"v2": "z"}, got)

		got, err = codec.Diff("", a(), squashPtr{ID: "1"})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"type": nil, "v2": nil}, got)

		assertPatch(t, "", a, changed)
		assertPatch(t, "", func() squashPtr { return squashPtr{ID: "1"} }, a())
	})

	t.Run("squash overwrite", func(t *testing.T) {
		t.Parallel()

		a := func() testOverwrite {
			return testOverwrite{Type: "p", V2: "p", Sub: testSub1{Type: "s", V2: "s"}}
		}

		b := a()
		b.V2, b.Sub.V2 = "q", "t"

		got, err := codec.Diff("", a(), b)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"v2": "t"}, got)

		assertPatch(t, "", a, b)
	})

	t.Run("marshalers", func(t *testing.T) {
		t.Parallel()

		a := func() testMarshalers {
			return testMarshalers{
				Price:   1250,
				Prices:  []testMoney{1, 2},
				Point:   testPoint{Lat: 1, Lon: 2},
				Profile: testProfile{Name: "n", Email: "e"},
			}
		}

		b := a()
		b.Price = 1300
		b.Point.Lat = 3
		b.Profile.Name = "m"

		got, err := codec.Diff("", a(), b)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"price": "13.00",
			"point": []float64{3, 2},
			"name":  "m",
			"email": "e", // a squashed Unmarshaler gets all of its keys
		}, got)

		assertPatch(t, "", a, b)

		got, err = codec.Diff("public", a(), b)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"price": "13.00", "point": []float64{3, 2}, "name": "m"}, got)
	})

	t.Run("root marshaler replaced whole", func(t *testing.T) {
		t.Parallel()

		a := func() testExtended { return testExtended{Type: "x", V2: "y"} }
		b := testExtended{Type: "z", V2: "y"}

		got, err := codec.Diff("", a(), b)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"type": "z", "v2": "y", "extended": true}, got)

		got, err = codec.Diff("", a(), a())
		require.NoError(t, err)
		assert.Nil(t, got)

		assertPatch(t, "", a, b)
	})

	t.Run("nil roots", func(t *testing.T) {
		t.Parallel()

		b := &testSub1{Type: "x", V2: "y"}

		got, err := codec.Diff("", nil, b)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"type": "x", "v2": "y"}, got)

		got, err = codec.Diff("", b, nil)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"type": nil, "v2": nil}, got)

		got, err = codec.Diff[*testSub1]("", nil, nil)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("pointer roots", func(t *testing.T) {
		t.Parallel()

		got, err := codec.Diff("", &testSub1{V2: "a"}, &testSub1{V2: "b"})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"v2": "b"}, got)
	})

	t.Run("interface roots", func(t *testing.T) {
		t.Parallel()

		got, err := codec.Diff[any]("", testSub1{V2: "a"}, &testSub1{V2: "b"})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"v2": "b"}, got)

		got, err = codec.Diff[any]("", testSub1{V2: "a"}, testSub2{Kind: "k"})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"v2": nil, "kind": "k"}, got)
	})

	t.Run("cycle", func(t *testing.T) {
		t.Parallel()

		cycle := func() *testNode {
			n := &testNode{Name: "a"}
			n.Next = n

			return n
		}

		_, err := codec.Diff("", cycle(), cycle())
		require.ErrorIs(t, err, structmap.ErrCycle)
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
		}{Any: testFailing{Bad: true}}}

		_, err := codec.Diff("", a, b)

		var fe *structmap.FieldError
		require.ErrorAs(t, err, &fe)
		assert.Equal(t, "sub.any", fe.Path)

		_, err = codec.Diff("", testMarshalers{Any: testFailing{Bad: true}}, testMarshalers{})
		require.ErrorAs(t, err, &fe)
		assert.Equal(t, "any", fe.Path)
		assert.EqualError(t, errors.Unwrap(err), "failing: bad")
	})

	t.Run("not a struct", func(t *testing.T) {
		t.Parallel()

		assert.Panics(t, func() { _, _ = codec.Diff("", 1, 2) })
		assert.PanicsWithValue(t, "structmap: empty tag", func() { _, _ = structmap.Codec{}.Diff("", testData{}, testData{}) })
	})
}

// assertPatch checks that the diff from a to b, unmarshaled into a, gives a value that marshals as b.
// a returns a new value each time, so the patch does not modify the pointers it shares with b.
func assertPatch[T any](t *testing.T, schema string, a func() T, b T) {
	t.Helper()

	patch, err := codec.Diff(schema, a(), b)
	require.NoError(t, err)

	got := a()
	require.NoError(t, codec.Unmarshal(schema, patch, &got))

	want, err := codec.Marshal(schema, b)
	require.NoError(t, err)

	gotMap, err := codec.Marshal(schema, got)
	require.NoError(t, err)

	assert.Equal(t, want, gotMap)
}
