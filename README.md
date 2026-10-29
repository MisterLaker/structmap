# structmap

Convert Go structs to `map[string]any` and back, driven by struct tags and **named schemas**.

One struct, several projections: a full document for storage, a reduced one for a search index,
a public one for an API, selected by the schema name at call time.

```bash
go get github.com/MisterLaker/structmap
```

Requires Go 1.27 or newer.

## Quick start

```go
type Address struct {
	City string `map:"city"`
	Zip  string `map:"zip,private"`
}

type User struct {
	ID        string    `map:"id"`
	Email     string    `map:"email,private"`
	Address   Address   `map:"address"`
	CreatedAt time.Time `map:"created_at,omitzero"`
}

user := User{ID: "42", Email: "a@example.com", Address: Address{City: "Kyiv", Zip: "01001"}}

public, err := structmap.Marshal("public", user)
// map[address:map[city:Kyiv] id:42]

full, err := structmap.Marshal("", user)
// map[address:map[city:Kyiv zip:01001] email:a@example.com id:42]

var decoded User
err = structmap.Unmarshal("", full, &decoded)
```

The package-level functions use the `map` tag (`structmap.DefaultTag`). For another tag, use a `Codec`:

```go
var db = structmap.Codec{Tag: "db"}

m, err := db.Marshal("search", doc)
err = db.Unmarshal("search", m, &doc)
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
| anything else | A schema name |

- A field without schema names is part of every schema, otherwise only of the listed ones.
- The empty schema `""` selects every tagged field.
- Untagged, unexported and `"-"` fields are skipped. An empty name falls back to the Go field name.
- Misspelled options (`omitempy`, `sqash`, `overwite`) panic instead of silently becoming schema names.

### Squash

```go
type Base struct {
	ID   string `map:"id"`
	Kind string `map:"kind"`
}

type Doc struct {
	Kind  string `map:"kind"`
	Base  Base   `map:",squash"`           // {"id", "kind"} merged, Doc.Kind wins
	Extra Base   `map:",squash,overwrite"` // its keys replace the parent's
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
	City string `map:"city"`
	Zip  string `map:"zip"`
}

type User struct {
	ID      string   `map:"id"`
	Name    string   `map:"name,omitempty"`
	Tags    []string `map:"tags"`
	Address Address  `map:"address"`
}

before := User{ID: "42", Name: "Ann", Tags: []string{"a"}, Address: Address{City: "Kyiv", Zip: "01001"}}
after := User{ID: "42", Tags: []string{"a", "b"}, Address: Address{City: "Lviv", Zip: "01001"}}

patch, err := structmap.Diff("", before, after)
// map[address:map[city:Lviv] name:<nil> tags:[a b]]

err = structmap.Unmarshal("", patch, &before) // before now marshals as after
```

- Only keys that differ are kept. Keys `b` drops (`omitempty`, `omitzero`) are `nil`, which resets the field.
- Nested structs present on both sides are diffed key by key, as `Unmarshal` merges into them.
- Slices, arrays, maps, `Marshaler` and `Unmarshaler` values are replaced whole.
  A squashed `Marshaler` or `Unmarshaler` gets all of its keys when one differs.
- Values are compared with their `Equal(T) bool` method when defined (`time.Time` of the same instant
  in another location is equal), with `reflect.DeepEqual` otherwise.
- The result is `nil` when nothing differs. `Codec.Diff` does the same with the codec's tag.

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

	m, err := s.Codec().Marshal(s.Schema, plain(d))
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
err := structmap.Unmarshal("", map[string]any{"qty": 300}, &item)
// qty: overflow: cannot decode 300 into uint8

var fe *structmap.FieldError
errors.As(err, &fe)                  // fe.Path == "qty"
errors.Is(err, structmap.ErrOverflow) // true
```

`Marshal` and `Diff` stop at the first error. `Unmarshal` decodes as much as possible and joins every error,
fields that decoded fine stay set.

Sentinels: `ErrType`, `ErrOverflow`, `ErrFraction`, `ErrNegative`, `ErrLength`, `ErrCycle`.

## Development

```bash
make container   # build the dev image
make up          # shell in the dev container, then:
make test lint bench
```

## License

MIT
