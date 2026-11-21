package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// OrderedKV is a single key-value entry in an OrderedObject.
type OrderedKV struct {
	Key   string
	Value any
}

// OrderedObject preserves JSON object key order across read/modify/write cycles.
type OrderedObject struct {
	Entries []OrderedKV
}

func NewOrderedObject() *OrderedObject {
	return &OrderedObject{}
}

func (o *OrderedObject) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	for _, kv := range o.Entries {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return nil, false
}

func (o *OrderedObject) Has(key string) bool {
	_, ok := o.Get(key)
	return ok
}

func (o *OrderedObject) Set(key string, val any) {
	for i := range o.Entries {
		if o.Entries[i].Key == key {
			o.Entries[i].Value = val
			return
		}
	}
	o.Entries = append(o.Entries, OrderedKV{Key: key, Value: val})
}

func (o *OrderedObject) Delete(key string) bool {
	for i := range o.Entries {
		if o.Entries[i].Key == key {
			o.Entries = append(o.Entries[:i], o.Entries[i+1:]...)
			return true
		}
	}
	return false
}

func (o *OrderedObject) Len() int {
	if o == nil {
		return 0
	}
	return len(o.Entries)
}

func (o *OrderedObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, kv := range o.Entries {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := marshalNoEscape(kv.Key)
		if err != nil {
			return nil, err
		}
		vb, err := marshalNoEscape(kv.Value)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}), nil
}

func marshalPrettyJSON(v any) ([]byte, error) {
	raw, err := marshalNoEscape(v)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// ParseOrderedJSON parses JSON while preserving object key order.
func ParseOrderedJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	val, err := decodeOrderedValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after JSON value")
	}
	return val, nil
}

func decodeOrderedValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	switch delim {
	case '{':
		obj := NewOrderedObject()
		for dec.More() {
			kTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := kTok.(string)
			if !ok {
				return nil, fmt.Errorf("expected object key, got %v", kTok)
			}
			val, err := decodeOrderedValue(dec)
			if err != nil {
				return nil, err
			}
			obj.Set(key, val)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return obj, nil
	case '[':
		arr := []any{}
		for dec.More() {
			val, err := decodeOrderedValue(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, val)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("unexpected delimiter %q", delim)
	}
}

func orderedEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	switch av := a.(type) {
	case *OrderedObject:
		bv, ok := b.(*OrderedObject)
		if !ok || av.Len() != bv.Len() {
			return false
		}
		for _, kv := range av.Entries {
			bval, exists := bv.Get(kv.Key)
			if !exists || !orderedEqual(kv.Value, bval) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !orderedEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case json.Number:
		switch bv := b.(type) {
		case json.Number:
			return av.String() == bv.String()
		case int64:
			n, err := av.Int64()
			return err == nil && n == bv
		case float64:
			f, err := av.Float64()
			return err == nil && f == bv
		default:
			return false
		}
	case int64:
		switch bv := b.(type) {
		case int64:
			return av == bv
		case json.Number:
			n, err := bv.Int64()
			return err == nil && n == av
		default:
			return false
		}
	case float64:
		switch bv := b.(type) {
		case float64:
			return av == bv
		case json.Number:
			f, err := bv.Float64()
			return err == nil && f == av
		default:
			return false
		}
	default:
		return a == b
	}
}

// StripJSONC removes // and /* */ comments and trailing commas from JSONC text.
func StripJSONC(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	inStr := false
	for i := 0; i < len(text); {
		c := text[i]
		if inStr {
			out.WriteByte(c)
			if c == '\\' && i+1 < len(text) {
				out.WriteByte(text[i+1])
				i += 2
				continue
			}
			if c == '"' {
				inStr = false
			}
			i++
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out.WriteByte('"')
			i++
		case c == '/' && i+1 < len(text) && text[i+1] == '/':
			for i < len(text) && text[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			i += 2
			for i+1 < len(text) && !(text[i] == '*' && text[i+1] == '/') {
				i++
			}
			if i+1 < len(text) {
				i += 2
			} else {
				i = len(text)
			}
		case c == ',':
			next := nextSignificant(text[i+1:])
			if next != '}' && next != ']' {
				out.WriteByte(',')
			}
			i++
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

func nextSignificant(rest string) byte {
	for {
		rest = strings.TrimLeft(rest, " \t\r\n")
		if strings.HasPrefix(rest, "//") {
			if idx := strings.IndexByte(rest, '\n'); idx >= 0 {
				rest = rest[idx+1:]
			} else {
				return 0
			}
		} else if strings.HasPrefix(rest, "/*") {
			if idx := strings.Index(rest[2:], "*/"); idx >= 0 {
				rest = rest[2+idx+2:]
			} else {
				return 0
			}
		} else {
			if len(rest) == 0 {
				return 0
			}
			return rest[0]
		}
	}
}
