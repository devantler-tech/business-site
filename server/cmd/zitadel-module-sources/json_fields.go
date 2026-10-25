package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"reflect"
	"slices"
	"strings"
)

// uniqueTypedFields rejects aliases of consumed fields, preserving map-key case.
// These schemas contain ordinary named fields, without embedded records.
func uniqueTypedFields(raw []byte, typ reflect.Type) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeFor[json.RawMessage]() || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		var values map[string]json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		fields := map[string]int{}
		for i := range typ.NumField() {
			field := typ.Field(i)
			if field.Anonymous {
				return fmt.Errorf("embedded metadata fields are unsupported")
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if field.PkgPath != "" || name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			fields[name] = i
		}
		seen := map[int]bool{}
		for name, value := range values {
			index, known := fields[name]
			if !known {
				for field, candidate := range fields {
					if strings.EqualFold(field, name) {
						index, known = candidate, true
						break
					}
				}
			}
			if !known {
				continue // Native compiler records include additional unconsumed fields.
			}
			if seen[index] {
				return fmt.Errorf("conflicting JSON field spellings")
			}
			seen[index] = true
			if err := uniqueTypedFields(value, typ.Field(index).Type); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		return uniqueTypedValues(slices.Values(values), typ.Elem())
	case reflect.Map:
		var values map[string]json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		return uniqueTypedValues(maps.Values(values), typ.Elem())
	}
	return nil
}

// uniqueTypedValues checks each collection value against its declared element type.
func uniqueTypedValues(values iter.Seq[json.RawMessage], typ reflect.Type) error {
	for value := range values {
		if err := uniqueTypedFields(value, typ); err != nil {
			return err
		}
	}
	return nil
}

// validateJSONFields applies literal-key and consumed-field checks before decoding.
func validateJSONFields(raw []byte, out any) error {
	if err := uniqueJSON(raw); err != nil {
		return err
	}
	return uniqueTypedFields(raw, reflect.TypeOf(out))
}

// decodeObservation retains native extra fields while refusing ambiguous input.
func decodeObservation(raw []byte, out any) error {
	if err := validateJSONFields(raw, out); err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
