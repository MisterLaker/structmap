package structmap

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type equalVector []float32

type equalCounter map[string]uint32

type equalStatus string

// TestFastEqual checks that fastEqual agrees with reflect.DeepEqual, which it replaces.
func TestFastEqual(t *testing.T) {
	t.Parallel()

	nan := math.NaN()
	shared := equalVector{1, float32(nan)}
	sharedMap := map[string]float64{"a": nan}
	counter := equalCounter{"a": 1, "b": 2}

	pairs := []struct {
		samePair

		fallback bool // left to reflect.DeepEqual
	}{
		{samePair: pair("vector", true, equalVector{1, 2}, equalVector{1, 2})},
		{samePair: pair("vector differ", true, equalVector{1, 2}, equalVector{1, 3})},
		{samePair: pair("vector length", true, equalVector{1, 2}, equalVector{1})},
		{samePair: pair("vector nan", true, equalVector{float32(nan)}, equalVector{float32(nan)})},
		{samePair: pair("vector nan shared", true, shared, shared)},
		{samePair: pair("vector zero signs", true, equalVector{0}, equalVector{float32(math.Copysign(0, -1))})},
		{samePair: pair("vector nil empty", true, equalVector(nil), equalVector{})},
		{samePair: pair("vector nils", true, equalVector(nil), equalVector(nil))},
		{samePair: pair("int8s", true, []int8{1, -1}, []int8{1, -1})},
		{samePair: pair("bools", true, []bool{true}, []bool{false})},
		{samePair: pair("strings", true, []string{"a", "b"}, []string{"a", "b"})},
		{samePair: pair("complexes", true, []complex128{1 + 2i}, []complex128{1 + 3i})},
		{samePair: pair("named items", true, []equalStatus{"a"}, []equalStatus{"a"})},
		{samePair: pair("bytes", true, []byte("ab"), []byte("ac"))},
		{samePair: pair("counter", true, equalCounter{"a": 1, "b": 2}, equalCounter{"b": 2, "a": 1})},
		{samePair: pair("counter differ", true, equalCounter{"a": 1, "b": 2}, equalCounter{"a": 1, "b": 3})},
		{samePair: pair("counter keys", true, equalCounter{"a": 1}, equalCounter{"b": 1})},
		{samePair: pair("counter length", true, equalCounter{"a": 1}, equalCounter{"a": 1, "b": 1})},
		{samePair: pair("counter nil empty", true, equalCounter(nil), equalCounter{})},
		{samePair: pair("counter shared", true, counter, counter)},
		{samePair: pair("map nan", true, map[string]float64{"a": nan}, map[string]float64{"a": nan})},
		{samePair: pair("map nan shared", true, sharedMap, sharedMap)},
		{samePair: pair("map strings", true, map[string]string{"a": "x"}, map[string]string{"a": "x"})},
		{samePair: pair("named keys", true, map[equalStatus]int{"a": 1}, map[equalStatus]int{"a": 1}), fallback: true},
		{samePair: pair("named values", true, map[string]equalStatus{"a": "x"}, map[string]equalStatus{"a": "y"}), fallback: true},
		{samePair: pair("slice values", true, map[string][]int{"a": {1}}, map[string][]int{"a": {1}}), fallback: true},
		{samePair: pair("times", true, []time.Time{{}}, []time.Time{{}}), fallback: true},
		{samePair: pair("pointers", true, []*int{nil}, []*int{nil}), fallback: true},
		{samePair: pair("array", true, [2]int{1, 2}, [2]int{1, 2}), fallback: true},
	}

	for _, p := range pairs {
		info := diffCodec.cache().info(p.x.Type())

		if p.fallback {
			assert.Nil(t, info.fastEqual, p.name)

			continue
		}

		require.NotNil(t, info.fastEqual, p.name)

		for _, addressable := range []bool{true, false} {
			x, y := p.x, p.y
			if !addressable {
				x, y = reflect.ValueOf(x.Interface()), reflect.ValueOf(y.Interface())
			}

			want := reflect.DeepEqual(x.Interface(), y.Interface())

			assert.Equal(t, want, info.fastEqual(x, y), "%s, addressable %v", p.name, addressable)
			assert.Equal(t, want, info.fastEqual(y, x), "%s reversed, addressable %v", p.name, addressable)
		}
	}
}

// TestFastEqualDiff checks that Diff takes the fast path for leaves and marshaled values.
func TestFastEqualDiff(t *testing.T) {
	t.Parallel()

	type leaves struct {
		Vector   equalVector  `tag:"vector"`
		Counters equalCounter `tag:"counters"`
	}

	a := func() leaves {
		return leaves{Vector: equalVector{1, 2}, Counters: equalCounter{"a": 1}}
	}

	got, err := diffCodec.Diff(a(), a(), "")
	require.NoError(t, err)
	assert.Nil(t, got)

	b := a()
	b.Vector[1] = 3
	b.Counters["b"] = 1

	got, err = diffCodec.Diff(a(), b, "")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"vector": b.Vector, "counters": b.Counters}, got)

	d := diffState{enc: encodeState{scope: diffCodec.scope(""), cache: diffCodec.cache()}}
	assert.True(t, d.equal(equalVector{1}, equalVector{1}))
	assert.False(t, d.equal(equalCounter{"a": 1}, equalCounter{"a": 2}))
}
