package structmap

import (
	"encoding"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

var (
	marshalerType       = reflect.TypeFor[Marshaler]()
	unmarshalerType     = reflect.TypeFor[Unmarshaler]()
	textMarshalerType   = reflect.TypeFor[encoding.TextMarshaler]()
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
	isZeroerType        = reflect.TypeFor[isZeroer]()
)

type isZeroer interface {
	IsZero() bool
}

type field struct {
	index     int
	name      string
	schemas   map[string]struct{} // nil means every schema
	omitEmpty bool
	omitZero  bool
	squash    bool
	overwrite bool // squashed keys replace the parent's
	zeroer    bool // the field type has IsZero, for omitzero
	ptrZeroer bool // only a pointer to the field type has IsZero
}

// omitted reports whether omitempty or omitzero skip the value v of the field.
func (f *field) omitted(v reflect.Value) bool {
	return f.omitEmpty && isEmptyValue(v) || f.omitZero && isZeroValue(v, f)
}

func (f *field) inSchema(schema string) bool {
	if schema == "" || f.schemas == nil {
		return true
	}

	_, ok := f.schemas[schema]

	return ok
}

// typeInfo is what conversions need to know about a type, computed once per tag and type.
type typeInfo struct {
	fields []field // tagged fields of a struct type
	simple bool    // every field has its own key: no squash and no duplicate names, for Diff

	marshaler       bool // T implements Marshaler
	ptrMarshaler    bool // only *T implements Marshaler
	unmarshaler     bool // *T implements Unmarshaler
	textUnmarshaler bool // *T implements encoding.TextUnmarshaler
	needsEncoding   bool // Marshal converts values of T, see needsEncoding
	equal           int  // index of the Equal(T) bool method of T, -1 if none, for Diff

	fastEqual func(x, y reflect.Value) bool // reflect.DeepEqual for values of T without reflecting on items, for Diff
}

// tagCache holds the type infos of one tag and Codec.Inline, it is resolved once per call
// so per-value lookups are keyed by type only.
type tagCache struct {
	tag    string
	inline bool
	types  sync.Map // reflect.Type -> *typeInfo
}

type cacheKey struct {
	tag    string
	inline bool
}

var tagCaches sync.Map // cacheKey -> *tagCache

func cacheFor(tag string, inline bool) *tagCache {
	key := cacheKey{tag: tag, inline: inline}

	if cached, ok := tagCaches.Load(key); ok {
		return cached.(*tagCache)
	}

	cached, _ := tagCaches.LoadOrStore(key, &tagCache{tag: tag, inline: inline})

	return cached.(*tagCache)
}

func (c *tagCache) info(t reflect.Type) *typeInfo {
	if cached, ok := c.types.Load(t); ok {
		return cached.(*typeInfo)
	}

	cached, _ := c.types.LoadOrStore(t, c.newInfo(t))

	return cached.(*typeInfo)
}

func (c *tagCache) newInfo(t reflect.Type) *typeInfo {
	info := &typeInfo{marshaler: t.Implements(marshalerType), equal: equalMethod(t)}

	if t.Kind() != reflect.Pointer {
		pt := reflect.PointerTo(t)
		info.ptrMarshaler = !info.marshaler && pt.Implements(marshalerType)
		info.unmarshaler = pt.Implements(unmarshalerType)
		info.textUnmarshaler = pt.Implements(textUnmarshalerType)
	}

	if t.Kind() == reflect.Struct {
		info.fields = parseFields(t, c.tag, c.inline)
		info.simple = simpleFields(info.fields)
		// Decided here, needsEncoding would look this struct up again.
		info.needsEncoding = info.marshaler || info.ptrMarshaler || len(info.fields) > 0
	} else {
		info.needsEncoding = c.needsEncoding(t, nil)
	}

	if !info.needsEncoding && info.equal < 0 {
		info.fastEqual = fastEqualFor(t)
	}

	return info
}

// simpleFields reports whether every field has its own key, so Diff compares them in place.
func simpleFields(fields []field) bool {
	for i := range fields {
		if fields[i].squash {
			return false
		}

		for j := range i {
			if fields[j].name == fields[i].name {
				return false
			}
		}
	}

	return true
}

// equalMethod returns the index of the Equal(T) bool method of t, such as time.Time's, or -1.
func equalMethod(t reflect.Type) int {
	if t.Kind() == reflect.Interface {
		return -1
	}

	m, ok := t.MethodByName("Equal")
	if !ok || m.Type.NumIn() != 2 || m.Type.In(1) != t || m.Type.NumOut() != 1 || m.Type.Out(0).Kind() != reflect.Bool {
		return -1
	}

	return m.Index
}

// needsEncoding reports whether values of t contain Marshalers, structs with tagged fields
// or maps with keys to format. Everything else (time.Time, []byte, scalars, ...) is kept as is.
// visiting guards recursive non-struct types such as type L []L.
func (c *tagCache) needsEncoding(t reflect.Type, visiting map[reflect.Type]struct{}) bool {
	if _, ok := visiting[t]; ok {
		return false
	}

	if visiting == nil {
		visiting = make(map[reflect.Type]struct{})
	}

	visiting[t] = struct{}{}

	if implementsMarshaler(t) {
		return true
	}

	switch t.Kind() {
	case reflect.Interface:
		return true
	case reflect.Struct:
		return c.info(t).needsEncoding
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return c.needsEncoding(t.Elem(), visiting)
	case reflect.Map:
		if !isEncodableKey(t.Key()) {
			return false
		}

		return t.Key().Kind() != reflect.String || c.needsEncoding(t.Elem(), visiting)
	default:
		return false
	}
}

func parseFields(t reflect.Type, tag string, inline bool) []field {
	var fields []field

	for i := range t.NumField() {
		sf := t.Field(i)

		value, ok := sf.Tag.Lookup(tag)
		if value == "-" {
			continue
		}

		inlined := inline && isInlined(sf, tag)
		if !inlined && (!ok || !sf.IsExported()) {
			continue
		}

		parts := strings.Split(value, ",")
		f := field{
			index:     i,
			name:      parts[0],
			zeroer:    sf.Type.Implements(isZeroerType),
			ptrZeroer: sf.Type.Kind() != reflect.Pointer && reflect.PointerTo(sf.Type).Implements(isZeroerType),
		}

		for _, item := range parts[1:] {
			switch item {
			case "":
			case "omitempty":
				f.omitEmpty = true
			case "omitzero":
				f.omitZero = true
			case "squash":
				f.squash = true
			case "overwrite":
				f.overwrite = true
			case "string":
				// encoding/json's option, ignored so that a codec on the json tag keeps the field in its schemas.
			default:
				if isOptionTypo(item) {
					panic(fmt.Sprintf("structmap: unknown option %q in %s.%s tag %q", item, t, sf.Name, tag))
				}

				if f.schemas == nil {
					f.schemas = make(map[string]struct{})
				}

				f.schemas[item] = struct{}{}
			}
		}

		if inlined {
			f.squash = true
		}

		switch {
		case f.squash:
			if !canSquash(sf.Type) {
				panic(fmt.Sprintf("structmap: squash on non-struct field %s.%s", t, sf.Name))
			}
		case f.overwrite:
			panic(fmt.Sprintf("structmap: overwrite without squash on %s.%s", t, sf.Name))
		case f.name == "":
			f.name = sf.Name
		}

		fields = append(fields, f)
	}

	return fields
}

// isInlined reports whether Codec.Inline squashes the field: an embedded struct, or pointer to one,
// whose tag has no name and which has keys to squash.
func isInlined(sf reflect.StructField, tag string) bool {
	name, _, _ := strings.Cut(sf.Tag.Get(tag), ",")

	return sf.Anonymous && name == "" && hasKeys(sf.Type, sf.IsExported(), tag, nil)
}

// hasKeys reports whether the embedded type t gives a parent keys when squashed, as parseFields
// reads it with Codec.Inline: it is a Marshaler or Unmarshaler, or has a field parseFields keeps.
// Types embedded through an unexported name are skipped when Unmarshal could not set them
// (a pointer it would allocate) or call them (a Marshaler or Unmarshaler).
// visiting guards types that embed themselves, such as type N struct{ *N }.
func hasKeys(t reflect.Type, exported bool, tag string, visiting map[reflect.Type]struct{}) bool {
	if t.Kind() == reflect.Pointer {
		if !exported {
			return false
		}

		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return false
	}

	if implementsMarshaler(t) || reflect.PointerTo(t).Implements(unmarshalerType) {
		return exported
	}

	if _, ok := visiting[t]; ok {
		return false
	}

	if visiting == nil {
		visiting = make(map[reflect.Type]struct{})
	}

	visiting[t] = struct{}{}

	for i := range t.NumField() {
		sf := t.Field(i)

		value, ok := sf.Tag.Lookup(tag)
		if value == "-" {
			continue
		}

		name, _, _ := strings.Cut(value, ",")
		if sf.Anonymous && name == "" && hasKeys(sf.Type, sf.IsExported(), tag, visiting) {
			return true
		}

		if ok && sf.IsExported() {
			return true
		}
	}

	return false
}

var options = [...]string{"omitempty", "omitzero", "squash", "overwrite"}

// isOptionTypo reports whether a tag item looks like a misspelled option rather than a schema name.
func isOptionTypo(item string) bool {
	if strings.HasPrefix(item, "omit") {
		return true
	}

	for _, option := range options {
		maxDistance := 1
		if len(option) > 6 {
			maxDistance = 2
		}

		if levenshtein(item, option) <= maxDistance {
			return true
		}
	}

	return false
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i

		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}

			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}

		prev, curr = curr, prev
	}

	return prev[len(b)]
}

// canSquash reports whether t is a struct or a Marshaler or Unmarshaler, possibly behind pointers.
func canSquash(t reflect.Type) bool {
	for {
		if implementsMarshaler(t) || reflect.PointerTo(t).Implements(unmarshalerType) {
			return true
		}

		if t.Kind() != reflect.Pointer {
			return t.Kind() == reflect.Struct
		}

		t = t.Elem()
	}
}

func implementsMarshaler(t reflect.Type) bool {
	return t.Implements(marshalerType) ||
		t.Kind() != reflect.Pointer && reflect.PointerTo(t).Implements(marshalerType)
}
