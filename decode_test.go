package structmap_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MisterLaker/structmap"
)

func TestUnmarshal(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	full := testData{
		ID:        "1",
		V1:        "a",
		V2:        "b",
		Sub1:      &testSub1{Type: "x", V1: "w", V2: "y"},
		Sub2:      testSub2{Kind: "k", Up: true},
		CreatedAt: ts,
	}

	roundTrips := []struct {
		schema string
		want   testData
	}{
		{schema: "schema1", want: full},
		{schema: "schema2", want: testData{ID: "1", V1: "a", Sub2: testSub2{Up: true}}},
		{schema: "", want: full},
		{schema: "other", want: testData{ID: "1"}},
	}

	for _, tt := range roundTrips {
		t.Run("round trip "+tt.schema, func(t *testing.T) {
			t.Parallel()

			var got testData
			require.NoError(t, codec.Unmarshal(marshal(t, tt.schema, full), &got, tt.schema))
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("json input", func(t *testing.T) {
		t.Parallel()

		var d map[string]any
		require.NoError(t, json.Unmarshal([]byte(`{
			"id": "1", "v1": "a", "sub": {"type": "x", "v2": "y"}, "kind": "k", "Up": true,
			"created_at": "2026-10-29T12:00:00Z"
		}`), &d))

		var got testData
		require.NoError(t, codec.Unmarshal(d, &got, ""))
		assert.Equal(t, testData{
			ID:        "1",
			V1:        "a",
			Sub1:      &testSub1{Type: "x", V2: "y"},
			Sub2:      testSub2{Kind: "k", Up: true},
			CreatedAt: ts,
		}, got)
	})

	t.Run("json leaves, slices and maps", func(t *testing.T) {
		t.Parallel()

		var d map[string]any
		require.NoError(t, json.Unmarshal([]byte(`{
			"Name": "n", "ptr": 42, "nil_ptr": null, "count": 3,
			"subs": [{"type": "a"}, {"v2": "b"}],
			"sub_ptrs": [{"type": "c"}, null],
			"sub_map": {"k": {"type": "d"}},
			"any": {"v2": "e"},
			"Skipped": "s", "Untagged": "u", "unexported": "x"
		}`), &d))

		var got testLeaves
		require.NoError(t, codec.Unmarshal(d, &got, ""))
		assert.Equal(t, testLeaves{
			Name:    "n",
			Ptr:     new(42),
			Count:   3,
			Subs:    []testSub1{{Type: "a"}, {V2: "b"}},
			SubPtrs: []*testSub1{{Type: "c"}, nil},
			SubMap:  map[string]testSub1{"k": {Type: "d"}},
			Any:     map[string]any{"v2": "e"},
		}, got)
	})

	t.Run("map keys round trip", func(t *testing.T) {
		t.Parallel()

		input := testMapKeys{
			Ints:   map[int]string{1: "a", -2: "b"},
			Uints:  map[uint8]string{3: "c"},
			Texts:  map[testKey]int{{A: "x", B: "y"}: 1},
			Plain:  map[string]int{"p": 1},
			Floats: map[float64]string{1.5: "f"},
		}

		var got testMapKeys
		require.NoError(t, codec.Unmarshal(marshal(t, "", input), &got, ""))
		assert.Equal(t, input, got)
	})

	t.Run("missing keys leave fields untouched", func(t *testing.T) {
		t.Parallel()

		sub := &testSub1{V1: "keep", V2: "keep"}
		got := testData{V1: "keep", Sub1: sub}

		require.NoError(t, codec.Unmarshal(map[string]any{
			"id":  "1",
			"sub": map[string]any{"type": "x"},
		}, &got, ""))

		assert.Equal(t, testData{ID: "1", V1: "keep", Sub1: &testSub1{Type: "x", V1: "keep", V2: "keep"}}, got)
		assert.Same(t, sub, got.Sub1, "existing pointer is reused")
	})

	t.Run("nil resets to zero", func(t *testing.T) {
		t.Parallel()

		got := testData{V1: "a", Sub1: &testSub1{}, CreatedAt: ts}

		require.NoError(t, codec.Unmarshal(map[string]any{
			"v1": nil, "sub": nil, "created_at": nil,
		}, &got, ""))

		assert.Equal(t, testData{}, got)
	})

	t.Run("fields outside schema are ignored", func(t *testing.T) {
		t.Parallel()

		var got testData
		require.NoError(t, codec.Unmarshal(map[string]any{
			"v2": "b", "kind": "k", "sub": map[string]any{"type": "x"},
		}, &got, "schema2"))

		assert.Equal(t, testData{}, got)
	})

	t.Run("squash pointer allocated only when set", func(t *testing.T) {
		t.Parallel()

		type squashPtr struct {
			ID  string    `tag:"id"`
			Sub *testSub1 `tag:",squash"`
		}

		var empty squashPtr
		require.NoError(t, codec.Unmarshal(map[string]any{"id": "1"}, &empty, ""))
		assert.Equal(t, squashPtr{ID: "1"}, empty)

		var set squashPtr
		require.NoError(t, codec.Unmarshal(map[string]any{"id": "1", "type": "x"}, &set, ""))
		assert.Equal(t, squashPtr{ID: "1", Sub: &testSub1{Type: "x"}}, set)
	})

	t.Run("squash collision sets both", func(t *testing.T) {
		t.Parallel()

		type collision struct {
			Type string   `tag:"type,omitempty"`
			V2   string   `tag:"v2"`
			Sub  testSub1 `tag:",squash"`
		}

		var got collision
		require.NoError(t, codec.Unmarshal(map[string]any{"type": "t", "v1": "w", "v2": "v"}, &got, ""))
		assert.Equal(t, collision{Type: "t", V2: "v", Sub: testSub1{Type: "t", V1: "w", V2: "v"}}, got)
	})

	t.Run("squash overwrite sets both", func(t *testing.T) {
		t.Parallel()

		var got testOverwrite
		require.NoError(t, codec.Unmarshal(map[string]any{"type": "t", "v2": "v"}, &got, "schema1"))
		assert.Equal(t, testOverwrite{Type: "t", V2: "v", Sub: testSub1{Type: "t", V2: "v"}}, got)
	})

	t.Run("numbers and named types", func(t *testing.T) {
		t.Parallel()

		var got testNumbers
		require.NoError(t, codec.Unmarshal(map[string]any{
			"i":   float64(3),
			"u":   int64(5),
			"f":   7,
			"s":   "ok",
			"b":   true,
			"arr": []any{1.0, int8(2)},
		}, &got, ""))

		assert.Equal(t, testNumbers{I: 3, U: 5, F: 7, S: "ok", B: true, Arr: [2]int{1, 2}}, got)
	})
}

func TestUnmarshalErrors(t *testing.T) {
	t.Parallel()

	t.Run("joined with paths, valid fields stay set", func(t *testing.T) {
		t.Parallel()

		var got testNumbers
		err := codec.Unmarshal(map[string]any{
			"i":   3.5,
			"u":   -1,
			"f":   "x",
			"s":   1,
			"b":   true,
			"arr": []any{1},
		}, &got, "")

		require.Error(t, err)
		assert.ErrorContains(t, err, "i: not an integer: cannot decode 3.5 into int8")
		assert.ErrorContains(t, err, "u: negative: cannot decode -1 into uint")
		assert.ErrorContains(t, err, "f: type mismatch: cannot decode string into float32")
		assert.ErrorContains(t, err, "s: type mismatch: cannot decode int into structmap_test.testStatus")
		assert.ErrorContains(t, err, "arr: length mismatch: cannot decode 1 items into [2]int")
		assert.Equal(t, testNumbers{B: true}, got)

		for _, sentinel := range []error{
			structmap.ErrFraction, structmap.ErrNegative, structmap.ErrType, structmap.ErrLength,
		} {
			assert.ErrorIs(t, err, sentinel)
		}
	})

	t.Run("single error is not joined", func(t *testing.T) {
		t.Parallel()

		var got testNumbers
		err := codec.Unmarshal(map[string]any{"i": 300}, &got, "")

		var fe *structmap.FieldError
		require.ErrorAs(t, err, &fe)
		assert.Equal(t, &structmap.FieldError{Path: "i", Err: fe.Err}, err)
		assert.ErrorIs(t, err, structmap.ErrOverflow)
	})

	t.Run("overflow", func(t *testing.T) {
		t.Parallel()

		var got testNumbers
		err := codec.Unmarshal(map[string]any{"i": 300, "u": 1e30}, &got, "")

		assert.ErrorContains(t, err, "i: overflow: cannot decode 300 into int8")
		assert.ErrorContains(t, err, "u: overflow: cannot decode 1e+30 into uint")
	})

	t.Run("nested paths", func(t *testing.T) {
		t.Parallel()

		var got testLeaves
		err := codec.Unmarshal(map[string]any{
			"subs":     []any{map[string]any{}, map[string]any{"v2": 1}},
			"sub_map":  map[string]any{"k": map[string]any{"type": true}},
			"sub_ptrs": []any{"x"},
		}, &got, "")

		assert.ErrorContains(t, err, "subs[1].v2: type mismatch: cannot decode int into string")
		assert.ErrorContains(t, err, "sub_map[k].type: type mismatch: cannot decode bool into string")
		assert.ErrorContains(t, err, "sub_ptrs[0]: type mismatch: cannot decode string into structmap_test.testSub1")
	})

	t.Run("map keys", func(t *testing.T) {
		t.Parallel()

		var got testMapKeys
		err := codec.Unmarshal(map[string]any{
			"ints":  map[string]any{"x": "a", "1": "b"},
			"uints": map[string]any{"300": "c"},
			"texts": map[string]any{"nocolon": 1},
		}, &got, "")

		assert.ErrorContains(t, err, `ints[x]: type mismatch: cannot decode key "x" into int`)
		assert.ErrorContains(t, err, `uints[300]: overflow: cannot decode key "300" into uint8`)
		assert.ErrorContains(t, err, "texts[nocolon]: key: missing colon")
		assert.Equal(t, map[int]string{1: "b"}, got.Ints)
	})

	t.Run("text unmarshaler", func(t *testing.T) {
		t.Parallel()

		var got testData
		err := codec.Unmarshal(map[string]any{"created_at": "yesterday"}, &got, "")

		assert.ErrorContains(t, err, "created_at: parsing time")
	})
}

func TestUnmarshalUnmarshaler(t *testing.T) {
	t.Parallel()

	input := testMarshalers{
		Price:    1250,
		Prices:   []testMoney{1, 2},
		PriceBy:  map[string]testMoney{"a": 3},
		Point:    testPoint{Lat: 1, Lon: 2},
		PointPtr: &testPoint{Lat: 3, Lon: 4},
		Profile:  testProfile{Name: "n", Email: "e"},
	}

	t.Run("round trip", func(t *testing.T) {
		t.Parallel()

		var got testMarshalers
		require.NoError(t, codec.Unmarshal(marshal(t, "", input), &got, ""))
		assert.Equal(t, input, got)
	})

	t.Run("json round trip", func(t *testing.T) {
		t.Parallel()

		data, err := json.Marshal(marshal(t, "", input))
		require.NoError(t, err)

		var d map[string]any
		require.NoError(t, json.Unmarshal(data, &d))

		var got testMarshalers
		require.NoError(t, codec.Unmarshal(d, &got, ""))
		assert.Equal(t, input, got)
	})

	t.Run("error with path", func(t *testing.T) {
		t.Parallel()

		var got testMarshalers
		err := codec.Unmarshal(map[string]any{"price": 5, "prices": []any{"0.01", true}}, &got, "")

		assert.ErrorContains(t, err, "price: money: unexpected int")
		assert.ErrorContains(t, err, "prices[1]: money: unexpected bool")
	})

	t.Run("top level", func(t *testing.T) {
		t.Parallel()

		var got testExtended
		require.NoError(t, codec.Unmarshal(map[string]any{
			"type": "a", "v2": "b", "extended": true,
		}, &got, "schema2"))
		assert.Equal(t, testExtended{Type: "a"}, got)

		assert.EqualError(t, codec.Unmarshal(map[string]any{}, &got, ""), "extended: missing marker")
	})
}

func TestUnmarshalPanics(t *testing.T) {
	t.Parallel()

	assert.PanicsWithValue(t, "structmap: Unmarshal expects a non-nil pointer, got structmap_test.testData",
		func() { _ = codec.Unmarshal(nil, testData{}, "") })
	assert.Panics(t, func() { _ = codec.Unmarshal(nil, (*testData)(nil), "") })
	assert.PanicsWithValue(t, "structmap: Unmarshal expects a pointer to struct, got *int",
		func() { _ = codec.Unmarshal(nil, new(int), "") })
}

func TestUnmarshalInline(t *testing.T) {
	t.Parallel()

	var got inlineDoc
	require.NoError(t, inlineCodec.Unmarshal(
		map[string]any{"lat": 50.45, "lon": 30.52, "note": "n", "id": "7", "city": "Kyiv"}, &got, ""))
	assert.Equal(t, newInlineDoc(), got)

	var partial inlineDoc
	require.NoError(t, inlineCodec.Unmarshal(map[string]any{"lat": 1.0, "city": "Lviv"}, &partial, ""))
	assert.Equal(t, inlineDoc{InlinePoint: InlinePoint{Lat: 1}, City: "Lviv"}, partial)
	assert.Nil(t, partial.InlineMeta, "allocated only when one of its keys is present")
}

func TestDiffInline(t *testing.T) {
	t.Parallel()

	a, b := newInlineDoc(), newInlineDoc()
	b.Lat = 49.84
	b.InlineMeta = &InlineMeta{}

	got, err := inlineCodec.Diff(a, b, "")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"lat": 49.84, "note": nil}, got)

	patched := newInlineDoc()
	require.NoError(t, inlineCodec.Unmarshal(got, &patched, ""))
	assert.Equal(t, b, patched)
}
