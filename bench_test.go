package structmap_test

import (
	"testing"
	"time"

	"github.com/MisterLaker/structmap"
)

type benchMeta struct {
	Author  string `map:"author"`
	Version int    `map:"version"`
}

type benchItem struct {
	SKU   string   `map:"sku"`
	Qty   int      `map:"qty,omitempty"`
	Price float64  `map:"price"`
	Tags  []string `map:"tags"`
}

type benchFlat struct {
	ID        string    `map:"id"`
	Title     string    `map:"title,public"`
	Body      string    `map:"body,full"`
	Views     int64     `map:"views,omitempty"`
	Active    bool      `map:"active"`
	CreatedAt time.Time `map:"created_at,omitzero"`
}

type benchNested struct {
	Flat   benchFlat         `map:",squash"`
	Meta   *benchMeta        `map:"meta"`
	Items  []benchItem       `map:"items"`
	Labels map[string]string `map:"labels"`
}

func newBenchNested() benchNested {
	items := make([]benchItem, 10)
	for i := range items {
		items[i] = benchItem{SKU: "sku", Qty: i, Price: 9.99, Tags: []string{"a", "b"}}
	}

	return benchNested{
		Flat:   benchFlat{ID: "1", Title: "t", Body: "b", Views: 7, Active: true, CreatedAt: time.Now()},
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
			_, _ = structmap.Marshal("map", "full", flat)
		}
	})

	b.Run("nested", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			_, _ = structmap.Marshal("map", "full", nested)
		}
	})
}

func BenchmarkUnmarshal(b *testing.B) {
	flat, _ := structmap.Marshal("map", "full", newBenchNested().Flat)
	nested, _ := structmap.Marshal("map", "full", newBenchNested())

	b.Run("flat", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var out benchFlat
			_ = structmap.Unmarshal("map", "full", flat, &out)
		}
	})

	b.Run("nested", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var out benchNested
			_ = structmap.Unmarshal("map", "full", nested, &out)
		}
	})
}
