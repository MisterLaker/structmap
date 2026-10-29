package structmap

import (
	"reflect"
	"slices"
)

// Diff is Diff with c.Tag.
func (c Codec) Diff[T any](schema string, a, b T) (map[string]any, error) {
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
	ea, err := d.entries(a)
	if err != nil {
		return nil, err
	}

	eb, err := d.entries(b)
	if err != nil {
		return nil, err
	}

	var result map[string]any

	set := func(k string, v any) {
		if result == nil {
			result = make(map[string]any)
		}

		result[k] = v
	}

	for k, y := range eb {
		x, ok := ea[k]
		if !ok {
			out, err := d.out(k, y)
			if err != nil {
				return nil, err
			}

			set(k, out)

			continue
		}

		changed, out, err := d.entryDiff(k, x, y)
		if err != nil {
			return nil, err
		}

		if changed {
			set(k, out)
		}
	}

	for k := range ea {
		if _, ok := eb[k]; !ok {
			set(k, nil)
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

		if f.omitEmpty && isEmptyValue(fv) || f.omitZero && isZeroValue(fv, f) {
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

	vx, vy := reflect.ValueOf(x), reflect.ValueOf(y)
	if vx.Type() != vy.Type() {
		return false
	}

	if i := d.enc.cache.info(vx.Type()).equal; i >= 0 {
		return vx.Method(i).Call([]reflect.Value{vy})[0].Bool()
	}

	return reflect.DeepEqual(x, y)
}
