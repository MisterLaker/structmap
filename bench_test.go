package structmap_test

import (
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
			_, _ = structmap.Marshal("full", flat)
		}
	})

	b.Run("nested", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			_, _ = structmap.Marshal("full", nested)
		}
	})
}

func BenchmarkUnmarshal(b *testing.B) {
	flat, _ := structmap.Marshal("full", newBenchNested().Flat)
	nested, _ := structmap.Marshal("full", newBenchNested())

	b.Run("flat", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var out benchFlat
			_ = structmap.Unmarshal("full", flat, &out)
		}
	})

	b.Run("nested", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var out benchNested
			_ = structmap.Unmarshal("full", nested, &out)
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
			_, _ = structmap.Diff("full", flatA, flatB)
		}
	})

	b.Run("nested", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			_, _ = structmap.Diff("full", nestedA, nestedB)
		}
	})
}
