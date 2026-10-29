package structmap

import (
	"encoding"
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"unsafe"
)

// startDetectingCyclesAfter is the pointer depth after which visited pointers are tracked,
// so cycle detection costs nothing for ordinary data, as in encoding/json.
const startDetectingCyclesAfter = 1000

type encodeState struct {
	scope    Scope
	cache    *tagCache
	path     path
	ptrLevel uint
	ptrSeen  map[cycleKey]struct{}
}

type cycleKey struct {
	ptr unsafe.Pointer
	typ reflect.Type
	len int
}

func (e *encodeState) marshal(v any) (map[string]any, error) {
	rv := reflect.ValueOf(v)

	for {
		if !rv.IsValid() {
			return nil, nil
		}

		if (rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface) && rv.IsNil() {
			return nil, nil
		}

		info := e.cache.info(rv.Type())

		if info.marshaler || info.ptrMarshaler {
			out, err := e.marshaler(rv, info)
			if err != nil {
				return nil, err
			}

			m, ok := out.(map[string]any)
			if !ok && out != nil {
				return nil, fmt.Errorf("%w: %s.MarshalMap returned %T, want map[string]any", ErrType, rv.Type(), out)
			}

			return m, nil
		}

		if rv.Kind() != reflect.Pointer && rv.Kind() != reflect.Interface {
			if rv.Kind() != reflect.Struct {
				panic(fmt.Sprintf("structmap: Marshal expects a struct, got %s", rv.Type()))
			}

			return e.structMap(rv, info)
		}

		rv = rv.Elem()
	}
}

// marshaler calls MarshalMap on v, whose type implements Marshaler on value or pointer receiver.
// Non-addressable values with a pointer receiver are copied, so Marshal(x) and Marshal(&x) behave the same.
func (e *encodeState) marshaler(v reflect.Value, info *typeInfo) (any, error) {
	switch {
	case info.ptrMarshaler && !v.CanAddr():
		c := reflect.New(v.Type()).Elem()
		c.Set(v)
		v = c.Addr()
	case info.ptrMarshaler || v.CanAddr() && v.Kind() != reflect.Pointer && v.Kind() != reflect.Interface:
		v = v.Addr()
	}

	out, err := v.Interface().(Marshaler).MarshalMap(e.scope)

	return out, e.path.wrap(err)
}

func (e *encodeState) structMap(v reflect.Value, info *typeInfo) (map[string]any, error) {
	fields := info.fields
	result := make(map[string]any, len(fields))

	var fills, overwrites []map[string]any

	for i := range fields {
		f := &fields[i]

		if !f.inSchema(e.scope.Schema) {
			continue
		}

		fv := v.Field(f.index)

		if f.squash {
			out, err := e.value(fv)
			if err != nil {
				return nil, err
			}

			if m, ok := out.(map[string]any); ok {
				if f.overwrite {
					overwrites = append(overwrites, m)
				} else {
					fills = append(fills, m)
				}
			}

			continue
		}

		if f.omitted(fv) {
			continue
		}

		e.path = append(e.path, segment{kind: segmentField, name: f.name})
		out, err := e.value(fv)
		e.path = e.path[:len(e.path)-1]

		if err != nil {
			return nil, err
		}

		result[f.name] = out
	}

	for _, m := range fills {
		for k, val := range m {
			if _, ok := result[k]; !ok {
				result[k] = val
			}
		}
	}

	for _, m := range overwrites {
		maps.Copy(result, m)
	}

	return result, nil
}

func (e *encodeState) value(v reflect.Value) (any, error) {
	if !v.IsValid() {
		return nil, nil
	}

	if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil() {
		return nil, nil
	}

	info := e.cache.info(v.Type())

	if info.marshaler || info.ptrMarshaler {
		return e.marshaler(v, info)
	}

	switch v.Kind() {
	case reflect.Interface:
		return e.value(v.Elem())

	case reflect.Pointer:
		if err := e.enter(v); err != nil {
			return nil, err
		}
		defer e.leave(v)

		return e.value(v.Elem())

	default:
	}

	if !info.needsEncoding {
		return v.Interface(), nil
	}

	switch v.Kind() {
	case reflect.Struct:
		return e.structMap(v, info)

	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice {
			if v.IsNil() {
				return nil, nil
			}

			if err := e.enter(v); err != nil {
				return nil, err
			}
			defer e.leave(v)
		}

		result := make([]any, v.Len())

		for i := range v.Len() {
			e.path = append(e.path, segment{kind: segmentIndex, index: i})
			out, err := e.value(v.Index(i))
			e.path = e.path[:len(e.path)-1]

			if err != nil {
				return nil, err
			}

			result[i] = out
		}

		return result, nil

	case reflect.Map:
		if v.IsNil() {
			return nil, nil
		}

		if err := e.enter(v); err != nil {
			return nil, err
		}
		defer e.leave(v)

		result := make(map[string]any, v.Len())

		iter := v.MapRange()
		for iter.Next() {
			key, err := mapKey(iter.Key())
			if err != nil {
				return nil, e.path.wrap(err)
			}

			e.path = append(e.path, segment{kind: segmentKey, name: key})
			out, err := e.value(iter.Value())
			e.path = e.path[:len(e.path)-1]

			if err != nil {
				return nil, err
			}

			result[key] = out
		}

		return result, nil

	default:
		return v.Interface(), nil
	}
}

// enter tracks reference values once the depth suggests a cycle, leave must follow a successful enter.
func (e *encodeState) enter(v reflect.Value) error {
	e.ptrLevel++
	if e.ptrLevel <= startDetectingCyclesAfter {
		return nil
	}

	if e.ptrSeen == nil {
		e.ptrSeen = make(map[cycleKey]struct{})
	}

	key := newCycleKey(v)
	if _, ok := e.ptrSeen[key]; ok {
		// No path, it is as deep as the cycle detection threshold.
		return fmt.Errorf("%w: %s", ErrCycle, v.Type())
	}

	e.ptrSeen[key] = struct{}{}

	return nil
}

func (e *encodeState) leave(v reflect.Value) {
	if e.ptrLevel > startDetectingCyclesAfter {
		delete(e.ptrSeen, newCycleKey(v))
	}

	e.ptrLevel--
}

func newCycleKey(v reflect.Value) cycleKey {
	key := cycleKey{ptr: v.UnsafePointer(), typ: v.Type()}
	if v.Kind() == reflect.Slice {
		key.len = v.Len()
	}

	return key
}

// isEncodableKey mirrors encoding/json map keys: strings, encoding.TextMarshaler and integers.
func isEncodableKey(t reflect.Type) bool {
	if t.Kind() == reflect.String || t.Implements(textMarshalerType) {
		return true
	}

	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	default:
		return false
	}
}

func mapKey(k reflect.Value) (string, error) {
	if k.Kind() == reflect.String {
		return k.String(), nil
	}

	if tm, ok := k.Interface().(encoding.TextMarshaler); ok {
		if k.Kind() == reflect.Pointer && k.IsNil() {
			return "", nil
		}

		b, err := tm.MarshalText()

		return string(b), err
	}

	if k.CanInt() {
		return strconv.FormatInt(k.Int(), 10), nil
	}

	return strconv.FormatUint(k.Uint(), 10), nil
}

// isEmptyValue mirrors encoding/json omitempty.
func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Interface, reflect.Pointer:
		return v.IsZero()
	default:
		return false
	}
}

// isZeroValue mirrors encoding/json omitzero: IsZero() when defined, reflect zero otherwise.
func isZeroValue(v reflect.Value, f *field) bool {
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return true
		}

		if z, ok := v.Interface().(isZeroer); ok {
			return z.IsZero()
		}

		return false
	}

	switch {
	case f.zeroer && !v.CanAddr():
		return v.Interface().(isZeroer).IsZero()
	case f.zeroer || f.ptrZeroer:
		if !v.CanAddr() {
			// Copy to call a pointer receiver, so Marshal(x) and Marshal(&x) agree, as in encoding/json.
			c := reflect.New(v.Type()).Elem()
			c.Set(v)
			v = c
		}

		return v.Addr().Interface().(isZeroer).IsZero()
	default:
		return v.IsZero()
	}
}
