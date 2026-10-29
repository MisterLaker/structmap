package structmap

import (
	"maps"
	"reflect"
	"slices"
	"unsafe"
)

// fastEqualFor returns a function comparing values of t as reflect.DeepEqual does without reflecting
// on every item, or nil: for slices of booleans, numbers and strings, and maps of them keyed by string.
func fastEqualFor(t reflect.Type) func(x, y reflect.Value) bool {
	switch t.Kind() {
	case reflect.Slice:
		return sliceEquals[t.Elem().Kind()]
	case reflect.Map:
		if t.Key() != stringType {
			return nil
		}

		return mapEquals[t.Elem()]
	default:
		return nil
	}
}

// sliceEquals is keyed by item kind: items of a named type have the layout of their kind.
var sliceEquals = map[reflect.Kind]func(x, y reflect.Value) bool{
	reflect.Bool:       sliceEqual[bool],
	reflect.Int:        sliceEqual[int],
	reflect.Int8:       sliceEqual[int8],
	reflect.Int16:      sliceEqual[int16],
	reflect.Int32:      sliceEqual[int32],
	reflect.Int64:      sliceEqual[int64],
	reflect.Uint:       sliceEqual[uint],
	reflect.Uint8:      sliceEqual[uint8],
	reflect.Uint16:     sliceEqual[uint16],
	reflect.Uint32:     sliceEqual[uint32],
	reflect.Uint64:     sliceEqual[uint64],
	reflect.Float32:    sliceEqual[float32],
	reflect.Float64:    sliceEqual[float64],
	reflect.Complex64:  sliceEqual[complex64],
	reflect.Complex128: sliceEqual[complex128],
	reflect.String:     sliceEqual[string],
}

var stringType = reflect.TypeFor[string]()

// mapEquals is keyed by the exact value type, as the key type is exactly string: the map is read
// as the map[string]V it is, only the name of the map type may differ. A named key type could hash
// differently, a named value type is left to reflect.DeepEqual for the same reason of exactness.
var mapEquals = map[reflect.Type]func(x, y reflect.Value) bool{
	reflect.TypeFor[bool]():       mapEqual[bool],
	reflect.TypeFor[int]():        mapEqual[int],
	reflect.TypeFor[int8]():       mapEqual[int8],
	reflect.TypeFor[int16]():      mapEqual[int16],
	reflect.TypeFor[int32]():      mapEqual[int32],
	reflect.TypeFor[int64]():      mapEqual[int64],
	reflect.TypeFor[uint]():       mapEqual[uint],
	reflect.TypeFor[uint8]():      mapEqual[uint8],
	reflect.TypeFor[uint16]():     mapEqual[uint16],
	reflect.TypeFor[uint32]():     mapEqual[uint32],
	reflect.TypeFor[uint64]():     mapEqual[uint64],
	reflect.TypeFor[float32]():    mapEqual[float32],
	reflect.TypeFor[float64]():    mapEqual[float64],
	reflect.TypeFor[complex64]():  mapEqual[complex64],
	reflect.TypeFor[complex128](): mapEqual[complex128],
	reflect.TypeFor[string]():     mapEqual[string],
}

// sliceEqual compares slices whose items have the kind of E as reflect.DeepEqual does:
// nil differs from empty, and a slice equals itself even with NaN items.
func sliceEqual[E comparable](x, y reflect.Value) bool {
	switch {
	case x.IsNil() || y.IsNil():
		return x.IsNil() && y.IsNil()
	case x.Len() != y.Len():
		return false
	case x.UnsafePointer() == y.UnsafePointer():
		return true
	}

	return slices.Equal(
		unsafe.Slice((*E)(x.UnsafePointer()), x.Len()),
		unsafe.Slice((*E)(y.UnsafePointer()), y.Len()),
	)
}

// mapEqual compares maps of type map[string]V, possibly named, as reflect.DeepEqual does:
// nil differs from empty, and a map equals itself even with NaN values.
func mapEqual[V comparable](x, y reflect.Value) bool {
	switch {
	case x.IsNil() || y.IsNil():
		return x.IsNil() && y.IsNil()
	case x.Len() != y.Len():
		return false
	case x.UnsafePointer() == y.UnsafePointer():
		return true
	}

	// A map value is a pointer to the runtime map.
	px, py := x.UnsafePointer(), y.UnsafePointer()

	return maps.Equal(*(*map[string]V)(unsafe.Pointer(&px)), *(*map[string]V)(unsafe.Pointer(&py)))
}
