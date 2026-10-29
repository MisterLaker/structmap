package structmap_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MisterLaker/structmap"
)

func TestMarshal(t *testing.T) {
	t.Parallel()

	// collision squashes testSub1 under keys the parent also sets.
	type collision struct {
		Type string   `tag:"type,omitempty"`
		V2   string   `tag:"v2"`
		Sub  testSub1 `tag:",squash"`
	}

	ts := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	full := testData{
		ID:        "1",
		V1:        "a",
		V2:        "b",
		Sub1:      &testSub1{Type: "x", V2: "y"},
		Sub2:      testSub2{Kind: "k", Up: true},
		CreatedAt: ts,
	}

	tests := []struct {
		name   string
		schema string
		input  any
		want   map[string]any
	}{
		{
			name:   "schema1",
			schema: "schema1",
			input:  full,
			want: map[string]any{
				"id":         "1",
				"v1":         "a",
				"v2":         "b",
				"sub":        map[string]any{"type": "x", "v2": "y"},
				"kind":       "k",
				"Up":         true,
				"created_at": ts,
			},
		},
		{
			name:   "schema2",
			schema: "schema2",
			input:  &full,
			want: map[string]any{
				"id": "1",
				"v1": "a",
				"Up": true,
			},
		},
		{
			name:   "empty schema includes every field",
			schema: "",
			input:  full,
			want: map[string]any{
				"id":         "1",
				"v1":         "a",
				"v2":         "b",
				"sub":        map[string]any{"type": "x", "v2": "y"},
				"kind":       "k",
				"Up":         true,
				"created_at": ts,
			},
		},
		{
			name:   "unknown schema keeps schemaless fields only",
			schema: "other",
			input:  full,
			want:   map[string]any{"id": "1"},
		},
		{
			name:   "omitempty and omitzero",
			schema: "schema1",
			input:  testData{},
			want:   map[string]any{"id": "", "kind": ""},
		},
		{
			name:   "nested struct keeps fields without omitempty",
			schema: "schema1",
			input:  testData{Sub1: &testSub1{}},
			want:   map[string]any{"id": "", "sub": map[string]any{"v2": ""}, "kind": ""},
		},
		{
			name:   "squash keys set by the parent win",
			schema: "",
			input:  collision{Type: "parent", V2: "pv", Sub: testSub1{Type: "sub", V2: "sv"}},
			want:   map[string]any{"type": "parent", "v2": "pv"},
		},
		{
			name:   "squash fills keys the parent did not set",
			schema: "",
			input:  collision{Sub: testSub1{Type: "sub", V2: "sv"}},
			want:   map[string]any{"type": "sub", "v2": ""},
		},
		{
			name:   "leaves, slices and maps",
			schema: "",
			input: testLeaves{
				Name:     "n",
				Ptr:      new(42),
				Bytes:    []byte("raw"),
				Subs:     []testSub1{{Type: "a"}, {V2: "b"}},
				SubPtrs:  []*testSub1{{Type: "c"}, nil},
				SubMap:   map[string]testSub1{"k": {Type: "d"}},
				Any:      testSub1{V2: "e"},
				Skipped:  "s",
				Untagged: "u",
			},
			want: map[string]any{
				"Name":     "n",
				"ptr":      42,
				"nil_ptr":  nil,
				"bytes":    []byte("raw"),
				"subs":     []any{map[string]any{"type": "a", "v2": ""}, map[string]any{"v2": "b"}},
				"sub_ptrs": []any{map[string]any{"type": "c", "v2": ""}, nil},
				"sub_map":  map[string]any{"k": map[string]any{"type": "d", "v2": ""}},
				"any":      map[string]any{"v2": "e"},
			},
		},
		{
			name:   "map keys",
			schema: "",
			input: testMapKeys{
				Ints:   map[int]string{1: "a", -2: "b"},
				Uints:  map[uint8]string{3: "c"},
				Texts:  map[testKey]int{{A: "x", B: "y"}: 1},
				Plain:  map[string]int{"p": 1},
				Floats: map[float64]string{1.5: "f"},
			},
			want: map[string]any{
				"ints":   map[string]any{"1": "a", "-2": "b"},
				"uints":  map[string]any{"3": "c"},
				"texts":  map[string]any{"x:y": 1},
				"plain":  map[string]int{"p": 1},
				"floats": map[float64]string{1.5: "f"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, marshal(t, tt.schema, tt.input))
		})
	}
}

func TestMarshalNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, marshal(t, "", nil))
	assert.Nil(t, marshal(t, "", (*testData)(nil)))
}

func TestMarshalOverwrite(t *testing.T) {
	t.Parallel()

	type twoOverwrites struct {
		First  testSub1 `tag:",squash,overwrite"`
		Second testSub1 `tag:",squash,overwrite"`
	}

	type mixed struct {
		Over testSub1 `tag:",squash,overwrite"`
		Fill testSub1 `tag:",squash"`
	}

	type profileOverwrite struct {
		Name    string      `tag:"name"`
		Profile testProfile `tag:",squash,overwrite"`
	}

	tests := []struct {
		name   string
		schema string
		input  any
		want   map[string]any
	}{
		{
			name:   "squashed keys replace the parent's",
			schema: "schema1",
			input:  testOverwrite{Type: "parent", V2: "pv", Sub: testSub1{Type: "sub", V2: "sv"}},
			want:   map[string]any{"type": "sub", "v2": "sv"},
		},
		{
			name:   "omitted squashed keys keep the parent's",
			schema: "schema1",
			input:  testOverwrite{Type: "parent", V2: "pv", Sub: testSub1{V2: "sv"}},
			want:   map[string]any{"type": "parent", "v2": "sv"},
		},
		{
			name:   "overwrite outside schema keeps the parent's",
			schema: "schema2",
			input:  testOverwrite{Type: "parent", V2: "pv", Sub: testSub1{Type: "sub", V2: "sv"}},
			want:   map[string]any{"type": "parent", "v2": "pv"},
		},
		{
			name:   "last overwrite wins",
			schema: "",
			input:  twoOverwrites{First: testSub1{Type: "a", V1: "w"}, Second: testSub1{Type: "b"}},
			want:   map[string]any{"type": "b", "v1": "w", "v2": ""},
		},
		{
			name:   "overwrite beats fill regardless of order",
			schema: "",
			input:  mixed{Over: testSub1{Type: "o"}, Fill: testSub1{Type: "f", V1: "fv"}},
			want:   map[string]any{"type": "o", "v1": "fv", "v2": ""},
		},
		{
			name:   "marshaler",
			schema: "",
			input:  profileOverwrite{Name: "parent", Profile: testProfile{Name: "n", Email: "e"}},
			want:   map[string]any{"name": "n", "email": "e"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, marshal(t, tt.schema, tt.input))
		})
	}
}

func TestMarshalMarshaler(t *testing.T) {
	t.Parallel()

	input := testMarshalers{
		Price:   1250,
		Prices:  []testMoney{1, 2},
		PriceBy: map[string]testMoney{"a": 3},
		Any:     testMoney(7),
		Point:   testPoint{Lat: 1, Lon: 2},
		Profile: testProfile{Name: "n", Email: "e"},
	}
	want := map[string]any{
		"price":    "12.50",
		"prices":   []any{"0.01", "0.02"},
		"price_by": map[string]any{"a": "0.03"},
		"any":      "0.07",
		"point":    []float64{1, 2},
		"name":     "n",
		"email":    "e",
	}

	t.Run("by value", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, want, marshal(t, "", input))
	})

	t.Run("by pointer", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, want, marshal(t, "", &input))
	})

	t.Run("scope is passed through", func(t *testing.T) {
		t.Parallel()

		got := marshal(t, "public", input)

		assert.Equal(t, "n", got["name"])
		assert.NotContains(t, got, "email")
	})

	t.Run("non-nil pointer field", func(t *testing.T) {
		t.Parallel()

		withPtr := input
		withPtr.PointPtr = &testPoint{Lat: 3, Lon: 4}

		assert.Equal(t, []float64{3, 4}, marshal(t, "", withPtr)["point_ptr"])
	})

	t.Run("top level", func(t *testing.T) {
		t.Parallel()

		want := map[string]any{"type": "a", "extended": true}

		assert.Equal(t, want, marshal(t, "schema2", testExtended{Type: "a", V2: "b"}))
		assert.Equal(t, want, marshal(t, "schema2", &testExtended{Type: "a", V2: "b"}))
	})

	t.Run("top level nil pointer is not called", func(t *testing.T) {
		t.Parallel()

		assert.Nil(t, marshal(t, "", (*testPoint)(nil)))
	})

	t.Run("top level non-map", func(t *testing.T) {
		t.Parallel()

		_, err := codec.Marshal("", testMoney(1))

		require.ErrorIs(t, err, structmap.ErrType)
		assert.EqualError(t, err,
			"type mismatch: structmap_test.testMoney.MarshalMap returned string, want map[string]any")
	})
}

func TestMarshalErrors(t *testing.T) {
	t.Parallel()

	t.Run("marshaler error with path", func(t *testing.T) {
		t.Parallel()

		type failing struct {
			Items map[string][]testFailing `tag:"items"`
		}

		_, err := codec.Marshal("", failing{
			Items: map[string][]testFailing{"k": {{}, {Bad: true}}},
		})

		var fe *structmap.FieldError
		require.ErrorAs(t, err, &fe)
		assert.Equal(t, "items[k][1]", fe.Path)
		assert.EqualError(t, err, "items[k][1]: failing: bad")
	})

	t.Run("cycle", func(t *testing.T) {
		t.Parallel()

		node := &testNode{Name: "a"}
		node.Next = &testNode{Name: "b", Next: node}

		_, err := codec.Marshal("", node)

		require.ErrorIs(t, err, structmap.ErrCycle)
	})

	t.Run("deep list without cycle", func(t *testing.T) {
		t.Parallel()

		var head *testNode
		for range 2000 {
			head = &testNode{Name: "n", Next: head}
		}

		_, err := codec.Marshal("", head)

		require.NoError(t, err)
	})
}

func TestMarshalPanics(t *testing.T) {
	t.Parallel()

	t.Run("non-struct input", func(t *testing.T) {
		t.Parallel()

		assert.Panics(t, func() { _, _ = codec.Marshal("", 42) })
	})

	t.Run("empty tag", func(t *testing.T) {
		t.Parallel()

		assert.PanicsWithValue(t, "structmap: empty tag", func() { _, _ = structmap.Codec{}.Marshal("", testData{}) })
	})

	t.Run("option typo", func(t *testing.T) {
		t.Parallel()

		type typo struct {
			CreatedAt time.Time `tag:"created_at,schema1,omitezero"`
		}

		assert.PanicsWithValue(t,
			`structmap: unknown option "omitezero" in structmap_test.typo.CreatedAt tag "tag"`,
			func() { _, _ = codec.Marshal("", typo{}) },
		)
	})

	t.Run("squash typo", func(t *testing.T) {
		t.Parallel()

		type typo struct {
			Sub testSub1 `tag:",sqash"`
		}

		assert.Panics(t, func() { _, _ = codec.Marshal("", typo{}) })
	})

	t.Run("squash on non-struct", func(t *testing.T) {
		t.Parallel()

		type badSquash struct {
			Name string `tag:",squash"`
		}

		assert.PanicsWithValue(t,
			"structmap: squash on non-struct field structmap_test.badSquash.Name",
			func() { _, _ = codec.Marshal("", badSquash{}) },
		)
	})

	t.Run("overwrite without squash", func(t *testing.T) {
		t.Parallel()

		type bad struct {
			Name string `tag:"name,overwrite"`
		}

		assert.PanicsWithValue(t,
			"structmap: overwrite without squash on structmap_test.bad.Name",
			func() { _, _ = codec.Marshal("", bad{}) },
		)
	})
}

func TestDefaultCodec(t *testing.T) {
	t.Parallel()

	type item struct {
		SKU string `map:"sku" tag:"id"`
		Qty int    `map:"qty,omitempty" tag:"qty"`
	}

	got, err := structmap.Marshal("", item{SKU: "a1"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"sku": "a1"}, got)

	var decoded item
	require.NoError(t, structmap.Unmarshal("", map[string]any{"sku": "b2", "id": "x", "qty": 5}, &decoded))
	assert.Equal(t, item{SKU: "b2", Qty: 5}, decoded)

	assert.PanicsWithValue(t, "structmap: empty tag", func() { _ = structmap.Codec{}.Unmarshal("", nil, &decoded) })
}

// testList is a recursive non-struct type, it must not loop while its type info is computed.
type testList []testList

func TestMarshalRecursiveType(t *testing.T) {
	t.Parallel()

	type holder struct {
		List testList `tag:"list"`
	}

	assert.Equal(t, map[string]any{"list": testList{nil, {}}}, marshal(t, "", holder{List: testList{nil, {}}}))
}

// testZeroPtr has IsZero on its pointer receiver only.
type testZeroPtr struct {
	N int
}

func (z *testZeroPtr) IsZero() bool {
	return z.N <= 0
}

func TestMarshalOmitZero(t *testing.T) {
	t.Parallel()

	type omitZero struct {
		Time    time.Time   `tag:"time,omitzero"`
		TimePtr *time.Time  `tag:"time_ptr,omitzero"`
		Ptr     testZeroPtr `tag:"ptr,omitzero"`
		Any     any         `tag:"any,omitzero"`
		Plain   int         `tag:"plain,omitzero"`
	}

	ts := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	zero := omitZero{TimePtr: &time.Time{}, Ptr: testZeroPtr{N: -1}, Any: time.Time{}}
	nonZero := omitZero{Time: ts, TimePtr: &ts, Ptr: testZeroPtr{N: 1}, Any: 1, Plain: 2}

	for name, input := range map[string]any{"by value": zero, "by pointer": &zero} {
		t.Run("zero "+name, func(t *testing.T) {
			t.Parallel()

			assert.Empty(t, marshal(t, "", input))
		})
	}

	assert.Equal(t, map[string]any{
		"time":     ts,
		"time_ptr": ts,
		"ptr":      testZeroPtr{N: 1},
		"any":      1,
		"plain":    2,
	}, marshal(t, "", nonZero))
}
