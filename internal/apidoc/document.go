// Package apidoc generates public response schemas from the actual Go types.
package apidoc

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"smapass-back/internal/domain/entity"
	"smapass-back/internal/interface/schema"
)

type object = map[string]any

// Generate combines hand-authored paths/constraints with Go-derived response schemas.
func Generate(source []byte) ([]byte, error) {
	var document object
	if err := json.Unmarshal(source, &document); err != nil {
		return nil, err
	}
	schemas := object{}
	var describe func(reflect.Type) (object, error)
	describe = func(t reflect.Type) (object, error) {
		if t == reflect.TypeFor[time.Time]() {
			return object{"type": "string", "format": "date-time"}, nil
		}
		switch t.Kind() {
		case reflect.Pointer:
			base, err := describe(t.Elem())
			return object{"anyOf": []any{base, object{"type": "null"}}}, err
		case reflect.Slice:
			item, err := describe(t.Elem())
			return object{"type": "array", "items": item}, err
		case reflect.Struct:
			name := t.Name()
			if name == "" {
				return nil, fmt.Errorf("anonymous response structs are unsupported")
			}
			ref := object{"$ref": "#/components/schemas/" + name}
			if _, exists := schemas[name]; exists {
				return ref, nil
			}
			properties := object{}
			required := []string{}
			schemas[name] = object{"type": "object", "properties": properties}
			for i := range t.NumField() {
				field := t.Field(i)
				if !field.IsExported() {
					continue
				}
				parts := strings.Split(field.Tag.Get("json"), ",")
				if parts[0] == "-" {
					continue
				}
				if parts[0] == "" {
					return nil, fmt.Errorf("%s.%s needs an explicit JSON tag", name, field.Name)
				}
				value, err := describe(field.Type)
				if err != nil {
					return nil, fmt.Errorf("%s.%s: %w", name, field.Name, err)
				}
				properties[parts[0]] = value
				if !strings.Contains(field.Tag.Get("json"), ",omitempty") {
					required = append(required, parts[0])
				}
			}
			schemas[name].(object)["required"] = required
			return ref, nil
		case reflect.String:
			return object{"type": "string"}, nil
		case reflect.Bool:
			return object{"type": "boolean"}, nil
		case reflect.Int, reflect.Int64:
			format := "int32"
			if t.Kind() == reflect.Int64 {
				format = "int64"
			}
			return object{"type": "integer", "format": format}, nil
		case reflect.Float64:
			return object{"type": "number", "format": "double"}, nil
		default:
			return nil, fmt.Errorf("unsupported public response type: %s", t)
		}
	}
	for _, root := range []any{entity.Certificate{}, entity.Device{}, entity.RegisterDevice{}, entity.TokenPair{}, entity.EnrollDevice{}, entity.RefreshRequest{}, entity.KeyChallenge{}, entity.EnrollmentChallengeRequest{}, entity.RotationChallengeRequest{}, schema.Session{}, schema.Certificates{}, schema.Records{}, schema.Error{}, schema.Health{}} {
		if _, err := describe(reflect.TypeOf(root)); err != nil {
			return nil, err
		}
	}
	overrides, ok := document["x-schema-overrides"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing x-schema-overrides")
	}
	for name, fields := range overrides {
		definition, exists := schemas[name].(object)
		if !exists {
			return nil, fmt.Errorf("unknown override schema %s", name)
		}
		for field, constraints := range fields.(map[string]any) {
			property, exists := definition["properties"].(object)[field].(object)
			if !exists {
				return nil, fmt.Errorf("unknown override field %s.%s", name, field)
			}
			for key, value := range constraints.(map[string]any) {
				property[key] = value
			}
		}
	}
	delete(document, "x-schema-overrides")
	document["components"] = object{"schemas": schemas}
	if schemes, ok := document["x-security-schemes"]; ok {
		document["components"].(object)["securitySchemes"] = schemes
		delete(document, "x-security-schemes")
	}
	result, err := json.MarshalIndent(document, "", "  ")
	return append(result, '\n'), err
}
