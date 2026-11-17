# structmap

Convert Go structs to `map[string]any` and back, driven by struct tags and **named schemas**.

One struct, several projections: a full document for storage, a reduced one for a search index,
a public one for an API, selected by the schema name at call time.

## Features

- **Named schemas**: one struct, several projections (storage, search index, public API), picked when you call it
- **Marshal / Unmarshal** to and from `map[string]any`, including nested structs, slices, maps and `TextMarshaler` keys
- **Diff** returns a patch from `a` to `b` that `Unmarshal` can apply. `Shallow` mode suits stores that replace top-level values
- **Works with JSON tags**: `structmap.JSON` reads `json` tags and inlines embedded structs the way `encoding/json` does
- **Tag options**: `omitempty`, `omitzero`, `squash`, `overwrite`. A misspelled option panics so it isn't treated as a schema name
- **Lenient decoding**: lossless numeric conversion, `TextUnmarshaler`, named types. Missing keys leave fields untouched
- **Custom types** through the `Marshaler` / `Unmarshaler` interfaces
- **Clear errors**: `*FieldError` gives the field path and wraps `ErrType` and `ErrOverflow`. `Unmarshal` collects every error instead of stopping at the first

```bash
go get github.com/MisterLaker/structmap
```

Requires Go 1.27 or newer.

## Quick start

```go
type Address struct {
	City string `structmap:"city"`
	Zip  string `structmap:"zip,private"`
}

type User struct {
	ID        string    `structmap:"id"`
	Email     string    `structmap:"email,private"`
	Address   Address   `structmap:"address"`
	CreatedAt time.Time `structmap:"created_at,omitzero"`
}

user := User{ID: "42", Email: "a@example.com", Address: Address{City: "Kyiv", Zip: "01001"}}

public, err := structmap.Marshal(user, "public")
// map[address:map[city:Kyiv] id:42]

full, err := structmap.Marshal(user, "")
// map[address:map[city:Kyiv zip:01001] email:a@example.com id:42]

var decoded User
err = structmap.Unmarshal(full, &decoded, "")
```

The package-level functions use the `structmap` tag (`structmap.DefaultTag`). For another tag, use a `Codec`:

```go
var db = structmap.Codec{Tag: "db"}

m, err := db.Marshal(doc, "search")
err = db.Unmarshal(m, &doc, "search")
```

## Tags

```
`<tag>:"name,item,..."`
```

| Item | Meaning |
|---|---|
| `omitempty` | Skip when empty: `false`, `0`, `""`, nil, empty slice or map (as `encoding/json`) |
| `omitzero` | Skip when zero, using the type's `IsZero()` when defined (as `encoding/json`) |
| `squash` | Merge the nested struct's keys into the parent instead of nesting them |
| `overwrite` | With `squash`: the squashed keys replace the parent's |
| `string` | Ignored: `encoding/json`'s option, so a codec on the `json` tag can read `json` tags |
| anything else | A schema name |

- A field without schema names is part of every schema, otherwise only of the listed ones.
- The empty schema `""` selects every tagged field.
- Untagged, unexported and `"-"` fields are skipped. An empty name falls back to the Go field name.
- Misspelled options (`omitempy`, `sqash`, `overwite`) panic instead of silently becoming schema names.

### Inline

A `Codec` with `Inline` squashes embedded structs whose tag has no name, as `encoding/json` inlines them,
so a codec on the `json` tag gives the keys `encoding/json` does:

```go
type Point struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type Place struct {
	Point        // squashed: lat, lon
	City  string `json:"city"`
}

m, err := structmap.Codec{Tag: "json", Inline: true}.Marshal(place, "")
// map[city:Kyiv lat:50.45 lon:30.52], without Inline map[city:Kyiv]
```

- Untagged embedded structs and ones tagged with options only (`json:",omitempty"`) are squashed,
  embedded structs of unexported types included.
- Embedded types without keys to squash (`sync.Mutex`) are skipped, and so are pointers to unexported types,
  which `Unmarshal` could not allocate.
- Precedence is the squash rule: the parent's keys win. `encoding/json` drops names ambiguous at the same depth instead.

### Squash

```go
type Base struct {
	ID   string `structmap:"id"`
	Kind string `structmap:"kind"`
}

type Doc struct {
	Kind  string `structmap:"kind"`
	Base  Base   `structmap:",squash"`           // {"id", "kind"} merged, Doc.Kind wins
	Extra Base   `structmap:",squash,overwrite"` // its keys replace the parent's
}
```

Without `overwrite` the parent's keys win and squashed keys only fill the gaps.
With `overwrite` the squashed keys win, the last such field wins among several.
When decoding, a key shared by the parent and a squashed struct is set on both.

## Conversion rules

**Marshal**

- Structs with tagged fields become `map[string]any`, slices and arrays of them `[]any`.
- Maps with integer or `encoding.TextMarshaler` keys become `map[string]any`, keys formatted as `encoding/json` does.
- Everything else (`time.Time`, scalars, `[]byte`, `map[string]int`, ...) is kept as is, pointers dereferenced.
- Cycles are reported as `ErrCycle` instead of overflowing the stack.

**Unmarshal**

- Only fields of the schema are set. Missing keys leave fields untouched, unknown keys are ignored,
  `nil` resets a field to its zero value.
- Values are assigned when assignable, decoded by `encoding.TextUnmarshaler` from strings
  (`time.Time` from RFC 3339), converted between numeric kinds when lossless, or converted to
  same-kind named types (`string` into `type Status string`).
- Nested maps decode into structs and maps (string, integer and `encoding.TextUnmarshaler` keys),
  slices into slices and arrays.

Unmarshal accepts its own Marshal output as well as `encoding/json` output (`float64` numbers, `[]any`, RFC 3339 strings).

## Diff

`Diff` returns a patch from `a` to `b`: unmarshaling it into `a` gives a value that marshals as `b`.

```go
type Address struct {
	City string `structmap:"city"`
	Zip  string `structmap:"zip"`
}

type User struct {
	ID      string   `structmap:"id"`
	Name    string   `structmap:"name,omitempty"`
	Tags    []string `structmap:"tags"`
	Address Address  `structmap:"address"`
}

before := User{ID: "42", Name: "Ann", Tags: []string{"a"}, Address: Address{City: "Kyiv", Zip: "01001"}}
after := User{ID: "42", Tags: []string{"a", "b"}, Address: Address{City: "Lviv", Zip: "01001"}}

patch, err := structmap.Diff(before, after, "")
// map[address:map[city:Lviv] name:<nil> tags:[a b]]

err = structmap.Unmarshal(patch, &before, "") // before now marshals as after
```

- Only keys that differ are kept. Keys `b` drops (`omitempty`, `omitzero`) are `nil`, which resets the field.
- Nested structs present on both sides are diffed key by key, as `Unmarshal` merges into them.
- Slices, arrays, maps, `Marshaler` and `Unmarshaler` values are replaced whole.
  A squashed `Marshaler` or `Unmarshaler` gets all of its keys when one differs.
- Values are compared with their `Equal(T) bool` method when defined (`time.Time` of the same instant
  in another location is equal), with `reflect.DeepEqual` otherwise. Slices of booleans, numbers and strings,
  and string-keyed maps of them, are compared as `reflect.DeepEqual` does without reflecting on every item.
- The result is `nil` when nothing differs. `Codec.Diff` does the same with the codec's tag.

### Shallow

Stores that replace top-level values instead of merging into them (a document update without object merging,
`$set`, one column per key) need changed nested structs whole. A `Codec` with `Shallow` returns them so:

```go
shallow := structmap.Codec{Tag: structmap.DefaultTag, Shallow: true}

patch, err := shallow.Diff(before, after, "")
// map[address:map[city:Lviv zip:01001] name:<nil> tags:[a b]]
```

Replacing `a`'s top-level values with the patch's gives what `b` marshals to. `Unmarshal` merges into nested structs,
so it does not apply such a patch exactly: a nested key `b` drops by `omitempty` or `omitzero` is not reset.

## Custom types

```go
type Money int64

func (m Money) MarshalMap(s structmap.Scope) (any, error) {
	return fmt.Sprintf("%d.%02d", m/100, m%100), nil
}

func (m *Money) UnmarshalMap(s structmap.Scope, v any) error {
	// parse v, a string here
}
```

`Scope` carries the tag and schema, so a type can produce schema dependent output.
A squashed `Marshaler` should return `map[string]any`, a squashed `Unmarshaler` gets the whole parent map.

To extend the default conversion instead of replacing it, convert a defined copy without methods
with `s.Codec()`, so the caller's tag is kept:

```go
func (d Doc) MarshalMap(s structmap.Scope) (any, error) {
	type plain Doc

	m, err := s.Codec().Marshal(plain(d), s.Schema)
	if err != nil {
		return nil, err
	}

	m["version"] = 2

	return m, nil
}
```

## Errors

Invalid arguments and invalid tags are programming errors and panic.
Data errors are returned as `*structmap.FieldError` with the path of the field:

```go
err := structmap.Unmarshal(map[string]any{"qty": 300}, &item, "")
// qty: overflow: cannot decode 300 into uint8

var fe *structmap.FieldError
errors.As(err, &fe)                  // fe.Path == "qty"
errors.Is(err, structmap.ErrOverflow) // true
```

`Marshal` and `Diff` stop at the first error. `Unmarshal` decodes as much as possible and joins every error,
fields that decoded fine stay set.

Sentinels: `ErrType`, `ErrOverflow`, `ErrFraction`, `ErrNegative`, `ErrLength`, `ErrCycle`.

## Benchmarks

| Benchmark | Time | Memory | Allocations |
|---|---|---|---|
| Marshal, flat | 387 ns | 464 B | 4 |
| Marshal, nested | 4.5 µs | 5488 B | 74 |
| Unmarshal, flat | 291 ns | 128 B | 2 |
| Unmarshal, nested | 2.6 µs | 976 B | 7 |
| Diff, flat | 508 ns | 224 B | 3 |
| Diff, nested | 5.0 µs | 3928 B | 20 |
| Diff, nested shallow | 5.0 µs | 3944 B | 21 |
| Diff, leaves | 2.7 µs | 544 B | 6 |

- **flat**: a struct of 6 fields (strings, `int64`, `bool`, `time.Time`).
- **nested**: the flat struct squashed, plus a pointer to a struct, a slice of 10 structs and a map.
- **leaves**: two 1024-item `[]float32` slices and a 64-entry `map[string]uint32`.
- **Diff** compares two independent values that differ in one field (flat, leaves) or two nested keys (nested).

Go 1.27, Apple M2 (linux/arm64 container), medians of 3 runs. Reproduce with `make bench`, see [bench_test.go](bench_test.go).

## Comparison

### Struct ↔ map conversion

| | structmap | [mapstructure] | [fatih/structs] | `encoding/json` round trip |
|---|---|---|---|---|
| Struct → map | ✓ | ✓ | ✓ | ✓ |
| Map → struct | ✓ | ✓ | – | ✓ |
| Named schemas (projections) | ✓ | – | – | – |
| Keeps Go types (`time.Time`, `int64`, `[]byte`) | ✓ | scalars, but `time.Time` becomes an empty map | nested structs become maps unless `omitnested` | – (`float64`, strings, base64) |
| Squash nested structs | `squash`, with `overwrite` precedence | `squash` | `flatten` | embedded structs only |
| `omitempty` / `omitzero` | both | both | `omitempty` | both (`omitzero` since Go 1.24) |
| Custom conversion per type | `Marshaler` / `Unmarshaler` with the schema | decode hooks | `string` option | `json.Marshaler` / `json.Unmarshaler` |
| Diff to a patch | ✓ | – | – | – |
| Maintained | ✓ | ✓ ([go-viper fork]) | archived, last change in 2018 | standard library |

- **[mapstructure]** is the usual choice for decoding loosely typed input such as configuration:
  weakly typed conversions (`"1"` into an `int`), decode hooks, `remain` for unknown keys and errors on unused keys.
  The original `mitchellh/mapstructure` was archived in 2024, development continues in the [go-viper fork].
  It has no notion of schemas, so each projection needs its own struct or post-processing.
- **[fatih/structs]** only converts structs to maps and is no longer maintained.
- **`encoding/json`** to `map[string]any` and back works without extra dependencies, but loses Go types:
  numbers become `float64`, `time.Time` a string, `[]byte` base64. It also costs a full encode and decode.
  structmap's `Unmarshal` accepts such maps, so the two can be mixed.

Use structmap when one struct has to produce several maps (storage, search index, public API) and
the values should keep their Go types.

### Diff

| | structmap `Diff` | [r3labs/diff] | [evanphx/json-patch] `CreateMergePatch` | [wI2L/jsondiff] | [go-cmp] |
|---|---|---|---|---|---|
| Input | Go values | Go values | JSON documents | JSON documents | Go values |
| Output | partial `map[string]any` | changelog of changes with paths | [RFC 7396] merge patch | [RFC 6902] JSON Patch operations | human-readable text |
| Applied with | `Unmarshal` | `diff.Patch` | `MergePatch` | a JSON Patch library | – (for tests) |
| Removed values | `nil` | delete changes | `null` | `remove` operations | `-` lines |
| Slices | replaced whole | per element, optionally by identifier | replaced whole | per element | per element |
| Schemas | ✓ | – | – | – | – |

`Diff` follows the semantics of an RFC 7396 merge patch, on Go values instead of JSON: nested objects
are merged, `nil` removes (resets the field to its zero value), slices and maps are replaced whole. The patch is in the same shape as `Marshal`
output, so it can be stored or sent as a partial update and applied back with `Unmarshal`.

Pick [r3labs/diff] for an audit log of individual changes, the JSON libraries when both sides are
already JSON, and [go-cmp] to compare values in tests.

[mapstructure]: https://github.com/go-viper/mapstructure
[go-viper fork]: https://github.com/go-viper/mapstructure
[fatih/structs]: https://github.com/fatih/structs
[r3labs/diff]: https://github.com/r3labs/diff
[evanphx/json-patch]: https://github.com/evanphx/json-patch
[wI2L/jsondiff]: https://github.com/wI2L/jsondiff
[go-cmp]: https://github.com/google/go-cmp
[RFC 7396]: https://www.rfc-editor.org/rfc/rfc7396
[RFC 6902]: https://www.rfc-editor.org/rfc/rfc6902

## Development

```bash
make container   # build the dev image
make up          # shell in the dev container, then:
make test lint bench
```

## License

MIT
