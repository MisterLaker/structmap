package structmap_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/MisterLaker/structmap"
)

type benchMeta struct {
	Author  string `structmap:"author"`
	Version int    `structmap:"version"`
}

type benchItem struct {
	SKU   string   `structmap:"sku"`
	Qty   int      `structmap:"qty,omitempty"`
	Price float64  `structmap:"price"`
	Tags  []string `structmap:"tags"`
}

type benchFlat struct {
	ID        string    `structmap:"id"`
	Title     string    `structmap:"title,public"`
	Body      string    `structmap:"body,full"`
	Views     int64     `structmap:"views,omitempty"`
	Active    bool      `structmap:"active"`
	CreatedAt time.Time `structmap:"created_at,omitzero"`
}

type benchNested struct {
	Flat   benchFlat         `structmap:",squash"`
	Meta   *benchMeta        `structmap:"meta"`
	Items  []benchItem       `structmap:"items"`
	Labels map[string]string `structmap:"labels"`
}

// benchLeaves holds large leaves: two slices of numbers and a map of them.
type benchLeaves struct {
	ID      string            `structmap:"id"`
	Weights []float32         `structmap:"weights"`
	Scores  []float32         `structmap:"scores"`
	Counts  map[string]uint32 `structmap:"counts"`
}

func newBenchLeaves() benchLeaves {
	numbers := func() []float32 {
		v := make([]float32, 1024)
		for i := range v {
			v[i] = float32(i) / 1024
		}

		return v
	}

	counts := make(map[string]uint32, 64)
	for i := range 64 {
		counts["key"+strconv.Itoa(i)] = uint32(i)
	}

	return benchLeaves{ID: "1", Weights: numbers(), Scores: numbers(), Counts: counts}
}

func newBenchNested() benchNested {
	items := make([]benchItem, 10)
	for i := range items {
		items[i] = benchItem{SKU: "sku", Qty: i, Price: 9.99, Tags: []string{"a", "b"}}
	}

	return benchNested{
		Flat:   benchFlat{ID: "1", Title: "t", Body: "b", Views: 7, Active: true, CreatedAt: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)},
		Meta:   &benchMeta{Author: "a", Version: 2},
		Items:  items,
		Labels: map[string]string{"k": "v"},
	}
}

func BenchmarkMarshal(b *testing.B) {
	flat := newBenchNested().Flat
	nested := newBenchNested()

	b.Run("flat", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			_, _ = structmap.Marshal(flat, "full")
		}
	})

	b.Run("nested", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			_, _ = structmap.Marshal(nested, "full")
		}
	})
}

func BenchmarkUnmarshal(b *testing.B) {
	flat, _ := structmap.Marshal(newBenchNested().Flat, "full")
	nested, _ := structmap.Marshal(newBenchNested(), "full")

	b.Run("flat", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var out benchFlat
			_ = structmap.Unmarshal(flat, &out, "full")
		}
	})

	b.Run("nested", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var out benchNested
			_ = structmap.Unmarshal(nested, &out, "full")
		}
	})
}

func BenchmarkDiff(b *testing.B) {
	// Independent values, so shared pointers do not shortcut the comparison.
	flatA, flatB := newBenchNested().Flat, newBenchNested().Flat
	flatB.Title = "changed"

	nestedA, nestedB := newBenchNested(), newBenchNested()
	nestedB.Meta.Version = 3
	nestedB.Labels["k"] = "w"

	b.Run("flat", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			_, _ = structmap.Diff(flatA, flatB, "full")
		}
	})

	b.Run("nested", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			_, _ = structmap.Diff(nestedA, nestedB, "full")
		}
	})

	shallow := structmap.Codec{Tag: structmap.DefaultTag, Shallow: true}

	b.Run("nested shallow", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			_, _ = shallow.Diff(nestedA, nestedB, "full")
		}
	})

	leavesA, leavesB := newBenchLeaves(), newBenchLeaves()
	leavesB.ID = "2"

	b.Run("leaves", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			_, _ = structmap.Diff(leavesA, leavesB, "")
		}
	})
}
