// Package structmap converts structs to map[string]any and back, driven by struct tags and named schemas.
//
// A schema selects which fields take part, so one struct can produce several projections,
// for example a full document for storage and a reduced one for a search index.
//
// # Tags
//
// The tag key is chosen by the caller (Marshal, Unmarshal or Codec.Tag). Its value is
//
//	`<tag>:"name,item,..."`
//
// where each item is an option or a schema name:
//
//   - omitempty: skip the field when empty, as encoding/json does (false, 0, "", nil, empty slice or map);
//   - omitzero: skip the field when zero, using its IsZero method when defined;
//   - squash: merge the nested struct (or Marshaler map) into the parent instead of nesting it;
//   - overwrite: with squash, the squashed keys replace the parent's;
//   - anything else is a schema name.
//
// A field without schema names takes part in every schema, otherwise only in the listed ones.
// An empty schema selects every tagged field. Untagged, unexported and "-" fields are skipped,
// an empty name falls back to the Go field name.
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
// # Custom types
//
// Types implementing Marshaler or Unmarshaler encode and decode themselves, including the value
// passed to Marshal or Unmarshal. To extend the default behaviour, call Marshal or Unmarshal on
// a defined type without methods (type plain T), calling it on T itself recurses forever.
//
// # Errors
//
// Invalid arguments and invalid tags are programming errors and panic. Data errors are returned:
// Marshal stops at the first one, Unmarshal decodes as much as possible and joins every error.
// Errors are reported as *FieldError with the field path and wrap sentinels such as ErrType and ErrOverflow.
package structmap
