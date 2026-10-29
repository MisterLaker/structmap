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

	marshaler       bool // T implements Marshaler
	ptrMarshaler    bool // only *T implements Marshaler
	unmarshaler     bool // *T implements Unmarshaler
	textUnmarshaler bool // *T implements encoding.TextUnmarshaler
	needsEncoding   bool // Marshal converts values of T, see needsEncoding
}

// tagCache holds the type infos of one tag, it is resolved once per call
// so per-value lookups are keyed by type only.
type tagCache struct {
	tag   string
	types sync.Map // reflect.Type -> *typeInfo
}

var tagCaches sync.Map // tag -> *tagCache

func cacheFor(tag string) *tagCache {
	if cached, ok := tagCaches.Load(tag); ok {
		return cached.(*tagCache)
	}

	cached, _ := tagCaches.LoadOrStore(tag, &tagCache{tag: tag})

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
	info := &typeInfo{marshaler: t.Implements(marshalerType)}

	if t.Kind() != reflect.Pointer {
		pt := reflect.PointerTo(t)
		info.ptrMarshaler = !info.marshaler && pt.Implements(marshalerType)
		info.unmarshaler = pt.Implements(unmarshalerType)
		info.textUnmarshaler = pt.Implements(textUnmarshalerType)
	}

	if t.Kind() == reflect.Struct {
		info.fields = parseFields(t, c.tag)
		// Decided here, needsEncoding would look this struct up again.
		info.needsEncoding = info.marshaler || info.ptrMarshaler || len(info.fields) > 0
	} else {
		info.needsEncoding = c.needsEncoding(t, nil)
	}

	return info
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

func parseFields(t reflect.Type, tag string) []field {
	var fields []field

	for i := range t.NumField() {
		sf := t.Field(i)

		value, ok := sf.Tag.Lookup(tag)
		if !ok || !sf.IsExported() || value == "-" {
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
