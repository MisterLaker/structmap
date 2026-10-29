package structmap

import (
	"reflect"
	"slices"
	"time"
)

// Diff is Diff with c.Tag.
func (c Codec) Diff[T any](a, b T, schema string) (map[string]any, error) {
	d := diffState{enc: encodeState{scope: c.scope(schema), cache: cacheFor(c.Tag)}}

	return d.diff(root(reflect.ValueOf(&a).Elem()), root(reflect.ValueOf(&b).Elem()))
}

// diffState walks two values of the same type, its encodeState marshals the values that differ.
type diffState struct {
	enc encodeState
}

// diffEntry is a key of a struct's Marshal output with the value it comes from.
type diffEntry struct {
	field *field        // the field the key comes from, nil for keys of a squashed group
	group *field        // the squash field of the Marshaler or Unmarshaler the key comes from
	v     reflect.Value // the field value, when field is set
	out   any           // the marshaled value, when group is set
}

// root unwraps interfaces, so Diff[any] compares the dynamic values.
func root(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}

	return v
}

func (d *diffState) diff(a, b reflect.Value) (map[string]any, error) {
	sa, okA := d.mergeable(a)
	sb, okB := d.mergeable(b)

	if okA && okB && sa.Type() == sb.Type() {
		return d.structDiff(sa, sb)
	}

	// Nil roots, Marshalers and Unmarshalers are replaced whole.
	ma, err := d.enc.marshal(a.Interface())
	if err != nil {
		return nil, err
	}

	mb, err := d.enc.marshal(b.Interface())
	if err != nil {
		return nil, err
	}

	if d.equal(ma, mb) {
		return nil, nil
	}

	result := make(map[string]any, len(mb))

	for k := range ma {
		result[k] = nil
	}

	for k, v := range mb {
		result[k] = v
	}

	return result, nil
}

// mergeable returns the struct v points to when Unmarshal merges a partial map into it:
// a struct with tagged fields that is neither a Marshaler nor an Unmarshaler.
func (d *diffState) mergeable(v reflect.Value) (reflect.Value, bool) {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return v, false
		}

		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return v, false
	}

	info := d.enc.cache.info(v.Type())

	return v, len(info.fields) > 0 && !info.marshaler && !info.ptrMarshaler && !info.unmarshaler
}

// structDiff returns the keys of b's Marshal output that differ from a's, nil for the keys a only has.
// Mergeable fields present on both sides are diffed into, the keys of a squashed group are
// replaced together, everything else is replaced whole.
func (d *diffState) structDiff(a, b reflect.Value) (map[string]any, error) {
	info := d.enc.cache.info(a.Type())
	if !info.simple {
		return d.entriesDiff(a, b)
	}

	// Each field has its own key, compare them in place.
	var result map[string]any

	for i := range info.fields {
		f := &info.fields[i]

		if !f.inSchema(d.enc.scope.Schema) {
			continue
		}

		x := diffEntry{field: f, v: a.Field(f.index)}
		y := diffEntry{field: f, v: b.Field(f.index)}
		omitX, omitY := f.omitted(x.v), f.omitted(y.v)

		var (
			changed = true
			out     any
			err     error
		)

		switch {
		case omitX && omitY:
			continue
		case omitY:
		case omitX:
			out, err = d.out(f.name, y)
		default:
			changed, out, err = d.entryDiff(f.name, x, y)
		}

		if err != nil {
			return nil, err
		}

		if changed {
			result = setKey(result, f.name, out)
		}
	}

	return result, nil
}

// entriesDiff is structDiff for structs whose keys come from several fields, through squash
// or duplicate names, it resolves them as Marshal does first.
func (d *diffState) entriesDiff(a, b reflect.Value) (map[string]any, error) {
	ea, err := d.entries(a)
	if err != nil {
		return nil, err
	}

	eb, err := d.entries(b)
	if err != nil {
		return nil, err
	}

	var result map[string]any

	for k, y := range eb {
		x, ok := ea[k]
		if !ok {
			out, err := d.out(k, y)
			if err != nil {
				return nil, err
			}

			result = setKey(result, k, out)

			continue
		}

		changed, out, err := d.entryDiff(k, x, y)
		if err != nil {
			return nil, err
		}

		if changed {
			result = setKey(result, k, out)
		}
	}

	for k := range ea {
		if _, ok := eb[k]; !ok {
			result = setKey(result, k, nil)
		}
	}

	// A squashed Unmarshaler decodes from the parent map, so a changed group gets all of its keys.
	var groups []*field

	for k := range result {
		for _, g := range [2]*field{ea[k].group, eb[k].group} {
			if g != nil && !slices.Contains(groups, g) {
				groups = append(groups, g)
			}
		}
	}

	for _, g := range groups {
		for k, y := range eb {
			if _, ok := result[k]; !ok && y.group == g {
				result[k] = y.out
			}
		}
	}

	return result, nil
}

// entryDiff compares the values a and b have under the key k.
func (d *diffState) entryDiff(k string, x, y diffEntry) (changed bool, out any, err error) {
	d.enc.path = append(d.enc.path, segment{kind: segmentField, name: k})
	defer func() { d.enc.path = d.enc.path[:len(d.enc.path)-1] }()

	if x.field != nil && x.field == y.field {
		sa, okA := d.mergeable(x.v)
		sb, okB := d.mergeable(y.v)

		if okA && okB {
			if x.v.Kind() == reflect.Pointer {
				if err := d.enc.enter(x.v); err != nil {
					return false, nil, err
				}
				defer d.enc.leave(x.v)
			}

			m, err := d.structDiff(sa, sb)

			return m != nil, m, err
		}
	}

	if x.field != nil && y.field != nil {
		return d.valueDiff(x.v, y.v)
	}

	outA, err := d.value(x)
	if err != nil {
		return false, nil, err
	}

	outB, err := d.value(y)
	if err != nil {
		return false, nil, err
	}

	return !d.equal(outA, outB), outB, nil
}

// valueDiff compares x and y and marshals y when they differ.
func (d *diffState) valueDiff(x, y reflect.Value) (changed bool, out any, err error) {
	same, ok, err := d.same(x, y)
	if err != nil {
		return false, nil, err
	}

	if ok {
		if same {
			return false, nil, nil
		}

		out, err := d.enc.value(y)

		return true, out, err
	}

	outX, err := d.enc.value(x)
	if err != nil {
		return false, nil, err
	}

	outY, err := d.enc.value(y)
	if err != nil {
		return false, nil, err
	}

	return !d.equal(outX, outY), outY, nil
}

// sameValue reports whether x and y marshal to equal values, marshaling them only when same cannot tell.
func (d *diffState) sameValue(x, y reflect.Value) (bool, error) {
	same, ok, err := d.same(x, y)
	if ok || err != nil {
		return same, err
	}

	outX, err := d.enc.value(x)
	if err != nil {
		return false, err
	}

	outY, err := d.enc.value(y)
	if err != nil {
		return false, err
	}

	return d.equal(outX, outY), nil
}

// same reports whether x and y marshal to equal values without marshaling them, as equal compares them.
// ok is false when it cannot tell: for Marshalers, values of different types or nil on one side only
// (the other may marshal to nil too), structs with squash fields and maps with encoding.TextMarshaler keys.
func (d *diffState) same(x, y reflect.Value) (same, ok bool, err error) {
	nilX, nilY := isNil(x), isNil(y)
	if nilX || nilY {
		return nilX && nilY, nilX && nilY, nil
	}

	if x.Type() != y.Type() {
		return false, false, nil
	}

	info := d.enc.cache.info(x.Type())

	if info.marshaler || info.ptrMarshaler {
		return false, false, nil
	}

	switch x.Kind() {
	case reflect.Interface:
		return d.same(x.Elem(), y.Elem())

	case reflect.Pointer:
		if x.UnsafePointer() == y.UnsafePointer() {
			return true, true, nil
		}

		if err := d.enc.enter(x); err != nil {
			return false, true, err
		}
		defer d.enc.leave(x)

		return d.same(x.Elem(), y.Elem())

	default:
	}

	if !info.needsEncoding {
		return d.leafSame(x, y, info), true, nil
	}

	switch x.Kind() {
	case reflect.Struct:
		if !info.simple {
			return false, false, nil
		}

		same, err := d.sameFields(x, y, info)

		return same, true, err

	case reflect.Slice:
		if x.IsNil() || y.IsNil() {
			return x.IsNil() && y.IsNil(), true, nil
		}

		if x.Len() != y.Len() {
			return false, true, nil
		}

		if x.UnsafePointer() == y.UnsafePointer() {
			return true, true, nil
		}

		if err := d.enc.enter(x); err != nil {
			return false, true, err
		}
		defer d.enc.leave(x)

		same, err := d.sameItems(x, y)

		return same, true, err

	case reflect.Array:
		same, err := d.sameItems(x, y)

		return same, true, err

	case reflect.Map:
		if x.IsNil() || y.IsNil() {
			return x.IsNil() && y.IsNil(), true, nil
		}

		if x.Len() != y.Len() {
			return false, true, nil
		}

		if x.UnsafePointer() == y.UnsafePointer() {
			return true, true, nil
		}

		if x.Type().Key().Kind() != reflect.String && x.Type().Key().Implements(textMarshalerType) {
			return false, false, nil
		}

		if err := d.enc.enter(x); err != nil {
			return false, true, err
		}
		defer d.enc.leave(x)

		same, err := d.sameEntries(x, y)

		return same, true, err

	default:
		return false, false, nil
	}
}

func (d *diffState) sameFields(x, y reflect.Value, info *typeInfo) (bool, error) {
	for i := range info.fields {
		f := &info.fields[i]

		if !f.inSchema(d.enc.scope.Schema) {
			continue
		}

		fx, fy := x.Field(f.index), y.Field(f.index)

		omitX, omitY := f.omitted(fx), f.omitted(fy)
		if omitX || omitY {
			if omitX != omitY {
				return false, nil
			}

			continue
		}

		d.enc.path = append(d.enc.path, segment{kind: segmentField, name: f.name})
		same, err := d.sameValue(fx, fy)
		d.enc.path = d.enc.path[:len(d.enc.path)-1]

		if err != nil || !same {
			return false, err
		}
	}

	return true, nil
}

func (d *diffState) sameItems(x, y reflect.Value) (bool, error) {
	for i := range x.Len() {
		d.enc.path = append(d.enc.path, segment{kind: segmentIndex, index: i})
		same, err := d.sameValue(x.Index(i), y.Index(i))
		d.enc.path = d.enc.path[:len(d.enc.path)-1]

		if err != nil || !same {
			return false, err
		}
	}

	return true, nil
}

// sameEntries compares maps of the same length with string or integer keys, which Marshal formats one to one.
func (d *diffState) sameEntries(x, y reflect.Value) (bool, error) {
	iter := x.MapRange()
	for iter.Next() {
		yv := y.MapIndex(iter.Key())
		if !yv.IsValid() {
			return false, nil
		}

		key, err := mapKey(iter.Key())
		if err != nil {
			return false, err
		}

		d.enc.path = append(d.enc.path, segment{kind: segmentKey, name: key})
		same, err := d.sameValue(iter.Value(), yv)
		d.enc.path = d.enc.path[:len(d.enc.path)-1]

		if err != nil || !same {
			return false, err
		}
	}

	return true, nil
}

// leafSame compares values Marshal keeps as is, as equal does, without boxing them when it can.
func (d *diffState) leafSame(x, y reflect.Value, info *typeInfo) bool {
	if info.equal >= 0 {
		if tx, ok := reflect.TypeAssert[time.Time](x); ok {
			ty, _ := reflect.TypeAssert[time.Time](y)

			return tx.Equal(ty)
		}

		return x.Method(info.equal).Call([]reflect.Value{y})[0].Bool()
	}

	switch x.Kind() {
	case reflect.Bool:
		return x.Bool() == y.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return x.Int() == y.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return x.Uint() == y.Uint()
	case reflect.Float32, reflect.Float64:
		return x.Float() == y.Float()
	case reflect.Complex64, reflect.Complex128:
		return x.Complex() == y.Complex()
	case reflect.String:
		return x.String() == y.String()
	default:
	}

	// Pointers to the values box without allocating and compare the same.
	if x.CanAddr() && y.CanAddr() {
		return reflect.DeepEqual(x.Addr().Interface(), y.Addr().Interface())
	}

	return reflect.DeepEqual(x.Interface(), y.Interface())
}

// out marshals the value of e under the key k.
func (d *diffState) out(k string, e diffEntry) (any, error) {
	d.enc.path = append(d.enc.path, segment{kind: segmentField, name: k})
	out, err := d.value(e)
	d.enc.path = d.enc.path[:len(d.enc.path)-1]

	return out, err
}

func (d *diffState) value(e diffEntry) (any, error) {
	if e.field == nil {
		return e.out, nil
	}

	return d.enc.value(e.v)
}

// entries mirrors encodeState.structMap without marshaling the fields.
func (d *diffState) entries(v reflect.Value) (map[string]diffEntry, error) {
	fields := d.enc.cache.info(v.Type()).fields
	result := make(map[string]diffEntry, len(fields))

	var fills, overwrites []map[string]diffEntry

	for i := range fields {
		f := &fields[i]

		if !f.inSchema(d.enc.scope.Schema) {
			continue
		}

		fv := v.Field(f.index)

		if f.squash {
			m, err := d.squashEntries(fv, f)
			if err != nil {
				return nil, err
			}

			if f.overwrite {
				overwrites = append(overwrites, m)
			} else {
				fills = append(fills, m)
			}

			continue
		}

		if f.omitted(fv) {
			continue
		}

		result[f.name] = diffEntry{field: f, v: fv}
	}

	for _, m := range fills {
		for k, e := range m {
			if _, ok := result[k]; !ok {
				result[k] = e
			}
		}
	}

	for _, m := range overwrites {
		for k, e := range m {
			result[k] = e
		}
	}

	return result, nil
}

func (d *diffState) squashEntries(v reflect.Value, f *field) (map[string]diffEntry, error) {
	if sv, ok := d.mergeable(v); ok {
		if v.Kind() == reflect.Pointer {
			if err := d.enc.enter(v); err != nil {
				return nil, err
			}
			defer d.enc.leave(v)
		}

		return d.entries(sv)
	}

	out, err := d.enc.value(v)
	if err != nil {
		return nil, err
	}

	m, _ := out.(map[string]any)
	result := make(map[string]diffEntry, len(m))

	for k, val := range m {
		result[k] = diffEntry{group: f, out: val}
	}

	return result, nil
}

// equal reports whether two marshaled values are equal, comparing leaves with their Equal method
// when they have one (time.Time ignores the location and monotonic clock) and reflect.DeepEqual otherwise.
func (d *diffState) equal(x, y any) bool {
	switch x := x.(type) {
	case map[string]any:
		y, ok := y.(map[string]any)
		if !ok || len(x) != len(y) || (x == nil) != (y == nil) {
			return false
		}

		for k, xv := range x {
			yv, ok := y[k]
			if !ok || !d.equal(xv, yv) {
				return false
			}
		}

		return true

	case []any:
		y, ok := y.([]any)
		if !ok || len(x) != len(y) || (x == nil) != (y == nil) {
			return false
		}

		for i := range x {
			if !d.equal(x[i], y[i]) {
				return false
			}
		}

		return true
	}

	if x == nil || y == nil {
		return x == nil && y == nil
	}

	if tx, ok := x.(time.Time); ok {
		ty, ok := y.(time.Time)

		return ok && tx.Equal(ty)
	}

	vx, vy := reflect.ValueOf(x), reflect.ValueOf(y)
	if vx.Type() != vy.Type() {
		return false
	}

	if i := d.enc.cache.info(vx.Type()).equal; i >= 0 {
		return vx.Method(i).Call([]reflect.Value{vy})[0].Bool()
	}

	return reflect.DeepEqual(x, y)
}

// isNil reports whether Marshal converts v to nil before looking at its type.
func isNil(v reflect.Value) bool {
	return !v.IsValid() || (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil()
}

func setKey(m map[string]any, k string, v any) map[string]any {
	if m == nil {
		m = make(map[string]any)
	}

	m[k] = v

	return m
}
