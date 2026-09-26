package guard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ParseBody decodes --data strictly: one JSON object, no key given twice at
// any depth, nothing after it. Numbers come back as json.Number, so every
// amount keeps the text it was written with. The guard and Meta must read
// the same values, and a duplicate key is where they would differ.
func ParseBody(data []byte) (map[string]any, error) {
	v, err := parseStrict(data)
	if err != nil {
		return nil, err
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("the body must be a JSON object")
	}
	return obj, nil
}

// parseStrict decodes one JSON value with the same rules.
func parseStrict(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec, "")
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after the JSON body")
	}
	return v, nil
}

func parseValue(dec *json.Decoder, path string) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil // string, json.Number, bool or nil
	}
	switch delim {
	case '{':
		obj := map[string]any{}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key := kt.(string)
			if _, dup := obj[key]; dup {
				return nil, fmt.Errorf("duplicate key %q at %s", key, where(path))
			}
			v, err := parseValue(dec, join(path, key))
			if err != nil {
				return nil, err
			}
			obj[key] = v
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return obj, nil
	case '[':
		arr := []any{}
		for i := 0; dec.More(); i++ {
			v, err := parseValue(dec, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return arr, nil
	}
	return nil, fmt.Errorf("unexpected %v at %s", delim, where(path))
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func where(path string) string {
	if path == "" {
		return "the top level"
	}
	return path
}
