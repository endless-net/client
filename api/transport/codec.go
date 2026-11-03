package transport

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"
)

// decode rejects duplicate keys before encoding/json can overwrite them and
// checks exact JSON field names (encoding/json alone accepts case aliases).
// Every non-omitempty field is required, including false, zero and empty arrays.
// Explicit null, trailing values and noncanonical base64 are never accepted.
func decode(raw []byte, limit int, target any) error {
	if len(raw) == 0 || len(raw) > limit || !utf8.Valid(raw) {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if checkTokens(d, 0) != nil {
		return ErrInvalid
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	if checkShape(raw, reflect.TypeOf(target).Elem(), 0) != nil {
		return ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil {
		return ErrInvalid
	}
	return nil
}

func checkTokens(d *json.Decoder, depth int) error {
	if depth > 8 {
		return ErrInvalid
	}
	token, err := d.Token()
	if err != nil || token == nil {
		return ErrInvalid
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]bool)
		for d.More() {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return ErrInvalid
			}
			seen[name] = true
			if checkTokens(d, depth+1) != nil {
				return ErrInvalid
			}
		}
	case '[':
		for d.More() {
			if checkTokens(d, depth+1) != nil {
				return ErrInvalid
			}
		}
	default:
		return ErrInvalid
	}
	end, err := d.Token()
	if err != nil || delimiter == '{' && end != json.Delim('}') || delimiter == '[' && end != json.Delim(']') {
		return ErrInvalid
	}
	return nil
}

func checkShape(raw json.RawMessage, t reflect.Type, depth int) error {
	if depth > 8 {
		return ErrInvalid
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return ErrInvalid
		}
		known := make(map[string]bool, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
			known[name] = true
			value, exists := fields[name]
			if !exists {
				if options != "omitempty" {
					return ErrInvalid
				}
				continue
			}
			if options == "omitempty" && (bytes.Equal(bytes.TrimSpace(value), []byte("0")) || bytes.Equal(bytes.TrimSpace(value), []byte(`""`))) {
				return ErrInvalid
			}
			if checkShape(value, field.Type, depth+1) != nil {
				return ErrInvalid
			}
		}
		for name := range fields {
			if !known[name] {
				return ErrInvalid
			}
		}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			var text string
			if json.Unmarshal(raw, &text) != nil {
				return ErrInvalid
			}
			value, err := base64.StdEncoding.Strict().DecodeString(text)
			if err != nil || base64.StdEncoding.EncodeToString(value) != text {
				return ErrInvalid
			}
		} else {
			var values []json.RawMessage
			if json.Unmarshal(raw, &values) != nil || values == nil {
				return ErrInvalid
			}
			for _, value := range values {
				if checkShape(value, t.Elem(), depth+1) != nil {
					return ErrInvalid
				}
			}
		}
	}
	return nil
}
