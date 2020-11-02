package gomerge

import (
	"errors"
	"fmt"
	"reflect"
)

var (
	errorInput          = errors.New("Check Inputs")
	errorDifferentTypes = errors.New("Different types of arguments")
)

func checkIngress(src, dst interface{}) error {
	if dst == nil || src == nil {
		return errorInput
	}
	if reflect.ValueOf(dst).Kind() != reflect.Ptr {
		return errorInput
	}
	return nil
}

func hasMergeableFields(dst reflect.Value) (exported bool) {
	for i, n := 0, dst.NumField(); i < n; i++ {
		field := dst.Type().Field(i)
		if field.Anonymous && dst.Field(i).Kind() == reflect.Struct {
			exported = exported || hasMergeableFields(dst.Field(i))
		} else if isExportedComponent(&field) {
			exported = exported || len(field.PkgPath) == 0
		}
	}
	return
}

func isExportedComponent(field *reflect.StructField) bool {
	pkgPath := field.PkgPath
	if len(pkgPath) > 0 {
		return false
	}
	c := field.Name[0]
	if 'a' <= c && c <= 'z' || c == '_' {
		return false
	}
	return true
}

// IsReflectNil is the reflect value provided nil
func isReflectNil(v reflect.Value) bool {
	k := v.Kind()
	switch k {
	case reflect.Interface, reflect.Slice, reflect.Chan, reflect.Func, reflect.Map, reflect.Ptr:
		// Both interface and slice are nil if first word is 0.
		// Both are always bigger than a word; assume flagIndir.
		return v.IsNil()
	default:
		return false
	}
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Ptr:
		if v.IsNil() {
			return true
		}
		return isEmptyValue(v.Elem())
	case reflect.Func:
		return v.IsNil()
	case reflect.Invalid:
		return true
	}
	return false
}

func conflate(src, dst reflect.Value) (err error) {
	switch dst.Kind() {
	case reflect.Struct:
		if hasMergeableFields(dst) {
			for i, n := 0, dst.NumField(); i < n; i++ {
				if err := conflate(dst.Field(i), src.Field(i)); err != nil {
					return err
				}
			}
		} else {
			if (isReflectNil(dst)) && (!isEmptyValue(src)) {
				dst.Set(src)
			}
		}
	case reflect.Map:
		if dst.IsNil() && !src.IsNil() {
			dst.Set(reflect.MakeMap(dst.Type()))
		}

		if src.Kind() != reflect.Map {
			return nil
		}

		for _, key := range src.MapKeys() {
			srcElement := src.MapIndex(key)
			if !srcElement.IsValid() {
				continue
			}
			dstElement := dst.MapIndex(key)
			switch srcElement.Kind() {
			case reflect.Chan, reflect.Func, reflect.Map, reflect.Interface, reflect.Slice:
				if srcElement.IsNil() {
					continue
				}
				fallthrough
			default:
				if !srcElement.CanInterface() {
					continue
				}
				switch reflect.TypeOf(srcElement.Interface()).Kind() {
				case reflect.Struct:
					fallthrough
				case reflect.Ptr:
					fallthrough
				case reflect.Map:
					srcMapElm := srcElement
					dstMapElm := dstElement
					if srcMapElm.CanInterface() {
						srcMapElm = reflect.ValueOf(srcMapElm.Interface())
						if dstMapElm.IsValid() {
							dstMapElm = reflect.ValueOf(dstMapElm.Interface())
						}
					}
					if err = conflate(dstMapElm, srcMapElm); err != nil {
						return
					}
				case reflect.Slice:
					srcSlice := reflect.ValueOf(srcElement.Interface())

					var dstSlice reflect.Value
					if !dstElement.IsValid() || dstElement.IsNil() {
						dstSlice = reflect.MakeSlice(srcSlice.Type(), 0, srcSlice.Len())
					} else {
						dstSlice = reflect.ValueOf(dstElement.Interface())
					}

					if (!isEmptyValue(src)) && (isEmptyValue(dst)) {
						if srcSlice.Type() != dstSlice.Type() {
							return fmt.Errorf("cannot override two slices with different type (%s, %s)", srcSlice.Type(), dstSlice.Type())
						}
						dstSlice = srcSlice
					}
					dst.SetMapIndex(key, dstSlice)
				}
			}
			if dstElement.IsValid() && !isEmptyValue(dstElement) && (reflect.TypeOf(srcElement.Interface()).Kind() == reflect.Map || reflect.TypeOf(srcElement.Interface()).Kind() == reflect.Slice) {
				continue
			}

			if srcElement.IsValid() && ((srcElement.Kind() != reflect.Ptr) || !dstElement.IsValid() || isEmptyValue(dstElement)) {
				if dst.IsNil() {
					dst.Set(reflect.MakeMap(dst.Type()))
				}
				dst.SetMapIndex(key, srcElement)
			}
		}
	case reflect.Slice:
		if !dst.CanSet() {
			break
		}
		if (!isEmptyValue(src)) && (isEmptyValue(dst)) {
			dst.Set(src)
		}
	case reflect.Ptr:
		switch src.Elem().Kind() {
		case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64, reflect.String, reflect.Slice:
			break
		}
		fallthrough
	case reflect.Interface:
		if isReflectNil(src) {
			if dst.CanSet() && src.Type().AssignableTo(dst.Type()) {
				dst.Set(src)
			}
			break
		}

		if src.Kind() != reflect.Interface {
			if dst.IsNil() || (src.Kind() != reflect.Ptr) {
				if dst.CanSet() && (isEmptyValue(dst)) {
					dst.Set(src)
				}
			} else if src.Kind() == reflect.Ptr {
				if err = conflate(dst.Elem(), src.Elem()); err != nil {
					return
				}
			} else if dst.Elem().Type() == src.Type() {
				if err = conflate(dst.Elem(), src); err != nil {
					return
				}
			} else {
				return errorInput
			}
			break
		}

		if dst.IsNil() {
			if dst.CanSet() && isEmptyValue(dst) {
				dst.Set(src)
			}
			break
		}

		if dst.Elem().Kind() == src.Elem().Kind() {
			if err = conflate(dst.Elem(), src.Elem()); err != nil {
				return
			}
			break
		}
	default:
		mustSet := (isEmptyValue(dst)) && (!isEmptyValue(src))
		if mustSet {
			if dst.CanSet() {
				dst.Set(src)
			} else {
				dst = src
			}
		}
	}
	return nil
}

// Merge is ...
func Merge(src, dst interface{}) error {
	if err := checkIngress(src, dst); err != nil {
		return err
	}
	var (
		vDst, vSrc reflect.Value
	)
	vDst = reflect.ValueOf(dst).Elem()
	if vDst.Kind() != reflect.Struct && vDst.Kind() != reflect.Map {
		return errorInput
	}
	vSrc = reflect.ValueOf(src)
	// Check if vSrc is a pointer to dereference it.
	if vSrc.Kind() == reflect.Ptr {
		vSrc = vSrc.Elem()
	}
	if vDst.Type() != vSrc.Type() {
		return errorDifferentTypes
	}
	return conflate(vSrc, vDst)
}
