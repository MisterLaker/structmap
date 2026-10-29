package structmap

import (
	"encoding"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
)

type decodeState struct {
	scope Scope
	cache *tagCache
	path  path
	errs  []error
}

func (d *decodeState) unmarshal(m map[string]any, out any) error {
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		panic(fmt.Sprintf("structmap: Unmarshal expects a non-nil pointer, got %T", out))
	}

	v = v.Elem()
	info := d.cache.info(v.Type())

	switch {
	case info.unmarshaler:
		d.unmarshaler(v, m)
	case v.Kind() == reflect.Struct:
		d.structValue(v, m, info)
	default:
		panic(fmt.Sprintf("structmap: Unmarshal expects a pointer to struct, got %T", out))
	}

	if len(d.errs) == 1 {
		return d.errs[0]
	}

	return errors.Join(d.errs...)
}

func (d *decodeState) fail(err error) {
	d.errs = append(d.errs, d.path.wrap(err))
}

// unmarshaler calls UnmarshalMap on the addressable v, whose pointer type implements Unmarshaler.
func (d *decodeState) unmarshaler(v reflect.Value, raw any) {
	if err := v.Addr().Interface().(Unmarshaler).UnmarshalMap(d.scope, raw); err != nil {
		d.fail(err)
	}
}

// structValue decodes m into the struct v, it reports whether any field was present in m.
func (d *decodeState) structValue(v reflect.Value, m map[string]any, info *typeInfo) (set bool) {
	fields := info.fields

	for i := range fields {
		f := &fields[i]

		if !f.inSchema(d.scope.Schema) {
			continue
		}

		fv := v.Field(f.index)

		if f.squash {
			set = d.squash(fv, m) || set

			continue
		}

		raw, ok := m[f.name]
		if !ok {
			continue
		}

		set = true

		d.path = append(d.path, segment{kind: segmentField, name: f.name})
		d.value(fv, raw)
		d.path = d.path[:len(d.path)-1]
	}

	return set
}

// squash decodes m into a squashed field, nil pointers are allocated only when a field was set.
func (d *decodeState) squash(v reflect.Value, m map[string]any) bool {
	if v.Kind() == reflect.Pointer {
		if !v.IsNil() {
			return d.squash(v.Elem(), m)
		}

		nv := reflect.New(v.Type().Elem())

		set := d.squash(nv.Elem(), m)
		if set {
			v.Set(nv)
		}

		return set
	}

	info := d.cache.info(v.Type())
	if info.unmarshaler {
		d.unmarshaler(v, m)

		return true
	}

	return d.structValue(v, m, info)
}

func (d *decodeState) value(v reflect.Value, raw any) {
	if raw == nil {
		v.SetZero()

		return
	}

	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			// Keep the partially decoded value on error, like the rest of the struct.
			v.Set(reflect.New(v.Type().Elem()))
		}

		d.value(v.Elem(), raw)

		return
	}

	info := d.cache.info(v.Type())
	if info.unmarshaler {
		d.unmarshaler(v, raw)

		return
	}

	rv := reflect.ValueOf(raw)

	if rv.Type().AssignableTo(v.Type()) {
		v.Set(rv)

		return
	}

	if s, ok := raw.(string); ok && info.textUnmarshaler {
		if err := v.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(s)); err != nil {
			d.fail(err)
		}

		return
	}

	switch v.Kind() {
	case reflect.Struct:
		if m, ok := raw.(map[string]any); ok {
			d.structValue(v, m, info)

			return
		}

	case reflect.Slice:
		if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
			s := reflect.MakeSlice(v.Type(), rv.Len(), rv.Len())
			d.items(s, rv)
			v.Set(s)

			return
		}

	case reflect.Array:
		if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
			if rv.Len() != v.Len() {
				d.fail(fmt.Errorf("%w: cannot decode %d items into %s", ErrLength, rv.Len(), v.Type()))

				return
			}

			v.SetZero()
			d.items(v, rv)

			return
		}

	case reflect.Map:
		if rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String && isDecodableKey(v.Type().Key()) {
			d.mapValue(v, rv)

			return
		}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		if err := decodeNumber(v, rv); err != nil {
			d.fail(err)
		}

		return

	case reflect.String, reflect.Bool:
		if rv.Kind() == v.Kind() {
			v.Set(rv.Convert(v.Type()))

			return
		}

	default:
	}

	d.fail(fmt.Errorf("%w: cannot decode %T into %s", ErrType, raw, v.Type()))
}

// items decodes the elements of the slice or array rv into dst of the same length.
func (d *decodeState) items(dst, rv reflect.Value) {
	for i := range rv.Len() {
		d.path = append(d.path, segment{kind: segmentIndex, index: i})
		d.value(dst.Index(i), rv.Index(i).Interface())
		d.path = d.path[:len(d.path)-1]
	}
}

func (d *decodeState) mapValue(v, rv reflect.Value) {
	t := v.Type()
	m := reflect.MakeMapWithSize(t, rv.Len())

	iter := rv.MapRange()
	for iter.Next() {
		key := iter.Key().String()

		d.path = append(d.path, segment{kind: segmentKey, name: key})

		if kv, err := decodeKey(t.Key(), key); err != nil {
			d.fail(err)
		} else {
			elem := reflect.New(t.Elem()).Elem()
			d.value(elem, iter.Value().Interface())
			m.SetMapIndex(kv, elem)
		}

		d.path = d.path[:len(d.path)-1]
	}

	v.Set(m)
}

// isDecodableKey mirrors encoding/json map keys: encoding.TextUnmarshaler, strings and integers.
func isDecodableKey(t reflect.Type) bool {
	if reflect.PointerTo(t).Implements(textUnmarshalerType) {
		return true
	}

	switch t.Kind() {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	default:
		return false
	}
}

func decodeKey(t reflect.Type, key string) (reflect.Value, error) {
	if reflect.PointerTo(t).Implements(textUnmarshalerType) {
		kv := reflect.New(t)
		if err := kv.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(key)); err != nil {
			return reflect.Value{}, err
		}

		return kv.Elem(), nil
	}

	kv := reflect.New(t).Elem()

	switch {
	case t.Kind() == reflect.String:
		kv.SetString(key)
	case kv.CanInt():
		n, err := strconv.ParseInt(key, 10, t.Bits())
		if err != nil {
			return reflect.Value{}, keyError(key, t, err)
		}

		kv.SetInt(n)
	default:
		n, err := strconv.ParseUint(key, 10, t.Bits())
		if err != nil {
			return reflect.Value{}, keyError(key, t, err)
		}

		kv.SetUint(n)
	}

	return kv, nil
}

func keyError(key string, t reflect.Type, err error) error {
	if errors.Is(err, strconv.ErrRange) {
		return fmt.Errorf("%w: cannot decode key %q into %s", ErrOverflow, key, t)
	}

	return fmt.Errorf("%w: cannot decode key %q into %s", ErrType, key, t)
}

// decodeNumber converts between numeric kinds, rejecting fractions, overflow and sign loss.
func decodeNumber(v, rv reflect.Value) error {
	t := v.Type()

	if !rv.CanInt() && !rv.CanUint() && !rv.CanFloat() {
		return fmt.Errorf("%w: cannot decode %s into %s", ErrType, rv.Type(), t)
	}

	switch {
	case v.CanInt():
		var n int64

		switch {
		case rv.CanInt():
			n = rv.Int()
		case rv.CanUint():
			if rv.Uint() > math.MaxInt64 {
				return fmt.Errorf("%w: cannot decode %v into %s", ErrOverflow, rv, t)
			}

			n = int64(rv.Uint())
		default:
			f := rv.Float()
			if f != math.Trunc(f) {
				return fmt.Errorf("%w: cannot decode %v into %s", ErrFraction, f, t)
			}

			if f < math.MinInt64 || f >= math.MaxInt64 {
				return fmt.Errorf("%w: cannot decode %v into %s", ErrOverflow, f, t)
			}

			n = int64(f)
		}

		if v.OverflowInt(n) {
			return fmt.Errorf("%w: cannot decode %v into %s", ErrOverflow, n, t)
		}

		v.SetInt(n)

	case v.CanUint():
		var n uint64

		switch {
		case rv.CanInt():
			if rv.Int() < 0 {
				return fmt.Errorf("%w: cannot decode %v into %s", ErrNegative, rv, t)
			}

			n = uint64(rv.Int())
		case rv.CanUint():
			n = rv.Uint()
		default:
			f := rv.Float()
			if f != math.Trunc(f) {
				return fmt.Errorf("%w: cannot decode %v into %s", ErrFraction, f, t)
			}

			if f < 0 {
				return fmt.Errorf("%w: cannot decode %v into %s", ErrNegative, f, t)
			}

			if f >= math.MaxUint64 {
				return fmt.Errorf("%w: cannot decode %v into %s", ErrOverflow, f, t)
			}

			n = uint64(f)
		}

		if v.OverflowUint(n) {
			return fmt.Errorf("%w: cannot decode %v into %s", ErrOverflow, n, t)
		}

		v.SetUint(n)

	default:
		var f float64

		switch {
		case rv.CanInt():
			f = float64(rv.Int())
		case rv.CanUint():
			f = float64(rv.Uint())
		default:
			f = rv.Float()
		}

		if v.OverflowFloat(f) {
			return fmt.Errorf("%w: cannot decode %v into %s", ErrOverflow, f, t)
		}

		v.SetFloat(f)
	}

	return nil
}
