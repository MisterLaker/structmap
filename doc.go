// Package structmap converts structs to map[string]any and back, driven by struct tags and named schemas.
//
// A schema selects which fields take part, so one struct can produce several projections,
// for example a full document for storage and a reduced one for a search index.
//
// # Tags
//
// Marshal, Unmarshal and Diff use the tag key "structmap" (DefaultTag), a Codec uses its own. Its value is
//
//	`<tag>:"name,item,..."`
//
// where each item is an option or a schema name:
//
//   - omitempty: skip the field when empty, as encoding/json does (false, 0, "", nil, empty slice or map);
//   - omitzero: skip the field when zero, using its IsZero method when defined;
//   - squash: merge the nested struct (or Marshaler map) into the parent instead of nesting it;
//   - overwrite: with squash, the squashed keys replace the parent's;
//   - string: ignored, it is encoding/json's option, so a Codec on the json tag can read json tags;
//   - anything else is a schema name.
//
// A field without schema names takes part in every schema, otherwise only in the listed ones.
// An empty schema selects every tagged field. Untagged, unexported and "-" fields are skipped,
// an empty name falls back to the Go field name.
//
// With Codec.Inline, embedded structs whose tag has no name (untagged, or options only such as ",omitempty")
// are squashed, as encoding/json inlines them, including embedded structs of unexported types.
// Embedded types with no keys to squash are skipped, and so are pointers to unexported types,
// which Unmarshal could not allocate. Precedence stays the squash rule: the parent's keys win,
// where encoding/json drops names that are ambiguous at the same depth.
//
// Items starting with "omit" or close to an option name (such as "sqash" or "overwite") panic,
// so a misspelled option is not silently taken as a schema name.
//
// # Marshal
//
// Structs with tagged fields become map[string]any, slices and arrays of them become []any,
// maps of them become map[string]any. Maps with integer or encoding.TextMarshaler keys become
// map[string]any with keys formatted as encoding/json does. Everything else (time.Time, scalars, []byte, ...)
// is kept as is, with pointers dereferenced.
//
// Squashed keys never replace keys the parent sets, unless the squash field has overwrite;
// then the last overwrite field wins.
//
// # Unmarshal
//
// Only fields of the schema are set. Keys missing from the map leave fields untouched,
// unknown keys are ignored and nil values reset fields to their zero value.
// Squashed structs decode from the parent map itself, keys shared with a parent field set both.
//
// Values are assigned when assignable, decoded by encoding.TextUnmarshaler from strings,
// converted between numeric kinds when lossless, or converted to same-kind named types
// (string to a named string type). Nested maps decode into structs and maps, slices into slices and arrays.
//
// # Diff
//
// Diff returns the patch from a to b: Unmarshal of the patch into a gives a value that marshals as b.
// The patch holds the keys of b's Marshal output that differ from a's, and nil for the keys only a has
// (removed by omitempty or omitzero), so Unmarshal resets them. It is nil when nothing differs.
//
// Nested structs present on both sides are diffed key by key, as Unmarshal merges into them.
// Everything else is replaced whole: slices, arrays, maps (Unmarshal rebuilds them), Marshalers
// and Unmarshalers. A squashed Marshaler or Unmarshaler gets all of its keys when one differs,
// as it decodes from the parent map.
//
// Values are compared by their Equal(T) bool method when defined, so time.Time values of the same
// instant are equal in any location, and by reflect.DeepEqual otherwise. Slices of booleans, numbers
// and strings, and string-keyed maps of them, are compared as reflect.DeepEqual does without reflecting
// on every item.
//
// With Codec.Shallow, nested structs are compared as a whole and a changed one is returned with all of its
// keys, for stores that replace top-level values instead of merging into them: replacing a's top-level
// values with the patch's gives what b marshals to. Unmarshal merges into nested structs, so it does not
// apply such a patch exactly: a nested key b drops by omitempty or omitzero is not reset.
//
// # Custom types
//
// Types implementing Marshaler or Unmarshaler encode and decode themselves, including the value
// passed to Marshal or Unmarshal. To extend the default behaviour, convert a defined type without
// methods (type plain T) with Scope.Codec, so the caller's tag and Inline are kept. Converting T itself recurses forever.
//
// # Errors
//
// Invalid arguments and invalid tags are programming errors and panic. Data errors are returned:
// Marshal and Diff stop at the first one, Unmarshal decodes as much as possible and joins every error.
// Errors are reported as *FieldError with the field path and wrap sentinels such as ErrType and ErrOverflow.
package structmap
