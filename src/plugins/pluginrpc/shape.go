package pluginrpc

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"reflect"
)

// gob intentionally omits zero struct fields and conflates nil and empty
// slices. Repository contracts compare immutable snapshots structurally, so
// these presence details are part of the wire contract, not presentation.
type dataShape struct {
	Present  bool               `json:",omitempty"`
	Absent   bool               `json:",omitempty"`
	Child    *dataShape         `json:",omitempty"`
	Fields   map[int]*dataShape `json:",omitempty"`
	Elements map[int]*dataShape `json:",omitempty"`
	Entries  []shapeEntry       `json:",omitempty"`
}

type shapeEntry struct {
	Key   []byte
	Shape *dataShape
}

func captureShape(v reflect.Value) (*dataShape, error) {
	return captureShapeAt(v, false, 0)
}

func shapeRelevant(typ reflect.Type) bool {
	switch typ.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map:
		return true
	case reflect.Array:
		return shapeRelevant(typ.Elem())
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.IsExported() && shapeRelevant(field.Type) {
				return true
			}
		}
	}
	return false
}

func captureShapeAt(v reflect.Value, preserveNil bool, depth int) (*dataShape, error) {
	if depth > 64 {
		return nil, fmt.Errorf("persistence value nesting exceeds its limit")
	}
	if !shapeRelevant(v.Type()) {
		return nil, nil
	}
	s := &dataShape{}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			if preserveNil {
				return &dataShape{Absent: true}, nil
			}
			return nil, nil
		}
		child, err := captureShapeAt(v.Elem(), true, depth+1)
		if err != nil {
			return nil, err
		}
		s.Present, s.Child = v.Elem().IsZero(), child
	case reflect.Slice:
		if v.IsNil() {
			if preserveNil {
				return &dataShape{Absent: true}, nil
			}
			return nil, nil
		}
		s.Present = v.Len() == 0
		fallthrough
	case reflect.Array:
		for i := 0; shapeRelevant(v.Type().Elem()) && i < v.Len(); i++ {
			child, err := captureShapeAt(v.Index(i), true, depth+1)
			if err != nil {
				return nil, err
			}
			if child != nil {
				if s.Elements == nil {
					s.Elements = map[int]*dataShape{}
				}
				s.Elements[i] = child
			}
		}
	case reflect.Map:
		if v.IsNil() {
			if preserveNil {
				return &dataShape{Absent: true}, nil
			}
			return nil, nil
		}
		s.Present = v.Len() == 0
		iter := v.MapRange()
		for shapeRelevant(v.Type().Elem()) && iter.Next() {
			child, err := captureShapeAt(iter.Value(), true, depth+1)
			if err != nil {
				return nil, err
			}
			if child != nil {
				var encoded bytes.Buffer
				if err = gob.NewEncoder(&encoded).EncodeValue(iter.Key()); err != nil {
					return nil, err
				}
				s.Entries = append(s.Entries, shapeEntry{Key: encoded.Bytes(), Shape: child})
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			child, err := captureShapeAt(v.Field(i), false, depth+1)
			if err != nil {
				return nil, err
			}
			if child != nil {
				if s.Fields == nil {
					s.Fields = map[int]*dataShape{}
				}
				s.Fields[i] = child
			}
		}
	}
	if !s.Present && s.Child == nil && len(s.Fields) == 0 && len(s.Elements) == 0 && len(s.Entries) == 0 {
		return nil, nil
	}
	return s, nil
}

func restoreShape(v reflect.Value, s *dataShape, depth int) error {
	if s == nil {
		return nil
	}
	if depth > 64 {
		return fmt.Errorf("persistence value nesting exceeds its limit")
	}
	if s.Absent {
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map:
			v.Set(reflect.Zero(v.Type()))
			return nil
		default:
			return fmt.Errorf("invalid absent persistence value")
		}
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return restoreShape(v.Elem(), s.Child, depth+1)
	case reflect.Interface:
		if v.IsNil() {
			return fmt.Errorf("missing persistence interface value")
		}
		value := reflect.New(v.Elem().Type()).Elem()
		value.Set(v.Elem())
		if err := restoreShape(value, s.Child, depth+1); err != nil {
			return err
		}
		v.Set(value)
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && s.Present && v.IsNil() {
			v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		}
		for index, child := range s.Elements {
			if index < 0 || index >= v.Len() {
				return fmt.Errorf("invalid persistence element shape")
			}
			if err := restoreShape(v.Index(index), child, depth+1); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for index, child := range s.Fields {
			if index < 0 || index >= v.NumField() || !v.Type().Field(index).IsExported() {
				return fmt.Errorf("invalid persistence field shape")
			}
			if err := restoreShape(v.Field(index), child, depth+1); err != nil {
				return err
			}
		}
	case reflect.Map:
		if s.Present && v.IsNil() {
			v.Set(reflect.MakeMap(v.Type()))
		}
		for _, entry := range s.Entries {
			key := reflect.New(v.Type().Key())
			if err := gob.NewDecoder(bytes.NewReader(entry.Key)).DecodeValue(key); err != nil {
				return fmt.Errorf("invalid persistence map key shape")
			}
			current := v.MapIndex(key.Elem())
			if !current.IsValid() {
				return fmt.Errorf("missing persistence map value")
			}
			value := reflect.New(v.Type().Elem()).Elem()
			value.Set(current)
			if err := restoreShape(value, entry.Shape, depth+1); err != nil {
				return err
			}
			v.SetMapIndex(key.Elem(), value)
		}
	}
	return nil
}
