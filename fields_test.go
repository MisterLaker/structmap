package structmap

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsOptionTypo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		item string
		want bool
	}{
		{item: "omitezero", want: true},
		{item: "omitempy", want: true},
		{item: "omit", want: true},
		{item: "omtempty", want: true},
		{item: "omitzer", want: true},
		{item: "sqash", want: true},
		{item: "squahs", want: false}, // transposition is two edits, too far for a short option
		{item: "overwite", want: true},
		{item: "ovrwrite", want: true},
		{item: "overwrites", want: true},
		{item: "squad", want: false},
		{item: "schema1", want: false},
		{item: "public", want: false},
		{item: "private", want: false},
		{item: "search", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.item, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, isOptionTypo(tt.item))
		})
	}
}

func TestLevenshtein(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, levenshtein("squash", "squash"))
	assert.Equal(t, 1, levenshtein("sqash", "squash"))
	assert.Equal(t, 2, levenshtein("squad", "squash"))
	assert.Equal(t, 6, levenshtein("", "squash"))
	assert.Equal(t, 3, levenshtein("kitten", "sitting"))
}

func TestPathString(t *testing.T) {
	t.Parallel()

	p := path{
		{kind: segmentField, name: "items"},
		{kind: segmentKey, name: "k"},
		{kind: segmentIndex, index: 2},
		{kind: segmentField, name: "name"},
	}

	assert.Equal(t, "items[k][2].name", p.String())
	assert.Empty(t, path(nil).String())
}
