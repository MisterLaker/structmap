package structmap

// Scope is the conversion context passed to Marshaler and Unmarshaler.
type Scope struct {
	Tag    string // struct tag key
	Schema string // selected schema, empty selects every field
	Inline bool   // Codec.Inline of the conversion
}

// Codec returns a Codec with the scope's tag and Inline, for Marshalers and Unmarshalers
// that extend the default conversion, see the package documentation.
func (s Scope) Codec() Codec {
	return Codec{Tag: s.Tag, Inline: s.Inline}
}

// Marshaler is implemented by types that convert themselves for Marshal.
// The returned value is used as is, a squashed Marshaler should return map[string]any.
type Marshaler interface {
	MarshalMap(s Scope) (any, error)
}

// Unmarshaler is implemented by types that decode themselves for Unmarshal.
// v is the value found under the field's key, a squashed Unmarshaler gets the whole parent map.
type Unmarshaler interface {
	UnmarshalMap(s Scope, v any) error
}

// Codec converts with a fixed struct tag key.
type Codec struct {
	Tag string

	// Inline squashes embedded structs whose tag has no name, untagged or with options only,
	// as encoding/json inlines them, see the package documentation.
	Inline bool

	// Shallow makes Diff compare nested structs as a whole and return a changed one with all of its keys,
	// for stores that replace top-level values instead of merging into them, see the package documentation.
	Shallow bool
}

// DefaultTag is the struct tag key of Marshal, Unmarshal and Diff.
const DefaultTag = "structmap"

var defaultCodec = Codec{Tag: DefaultTag}

var JSON = Codec{Tag: "json", Inline: true, Shallow: true}

// Marshal converts the struct v (or a pointer to one) into a map, see the package documentation.
// It returns nil for a nil v and panics when v is not a struct.
func Marshal(v any, schema string) (map[string]any, error) {
	return defaultCodec.Marshal(v, schema)
}

// Unmarshal decodes m into out, a non-nil pointer to a struct, see the package documentation.
// It panics when out is not a non-nil pointer to a struct or Unmarshaler.
func Unmarshal(m map[string]any, out any, schema string) error {
	return defaultCodec.Unmarshal(m, out, schema)
}

// Diff returns the patch from a to b, see the package documentation.
// It panics when T is not a struct or a pointer to one.
func Diff[T any](a, b T, schema string) (map[string]any, error) {
	return defaultCodec.Diff(a, b, schema)
}

// Marshal is Marshal with c.Tag.
func (c Codec) Marshal(v any, schema string) (map[string]any, error) {
	e := encodeState{scope: c.scope(schema), cache: c.cache()}

	return e.marshal(v)
}

// Unmarshal is Unmarshal with c.Tag.
func (c Codec) Unmarshal(m map[string]any, out any, schema string) error {
	d := decodeState{scope: c.scope(schema), cache: c.cache()}

	return d.unmarshal(m, out)
}

func (c Codec) scope(schema string) Scope {
	if c.Tag == "" {
		panic("structmap: empty tag")
	}

	return Scope{Tag: c.Tag, Schema: schema, Inline: c.Inline}
}

func (c Codec) cache() *tagCache {
	return cacheFor(c.Tag, c.Inline)
}
