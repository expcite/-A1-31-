// Package evidence contains shared measurement parsing and durable publication.
// It handles public result data only, never cryptographic session material.
package evidence

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"unicode/utf8"
)

const MaxFile = 64 << 20
const MaxLine = 64 << 10

// A round contains up to 1000 transaction facts; event lines keep MaxLine.
const MaxRow = 2 << 20

// Decode rejects duplicate keys at EVERY nesting level, trailing JSON, absent
// non-optional struct fields and unknown fields. Map users specify their own
// required event fields, because production events have optional metadata.
func Decode(b []byte, dst any) error {
	t := reflect.TypeOf(dst)
	if t == nil || t.Kind() != reflect.Pointer || reflect.ValueOf(dst).IsNil() {
		return errors.New("JSON destination must be a non-nil pointer")
	}
	if len(b) > MaxFile {
		return fmt.Errorf("JSON exceeds size limit")
	}
	if !utf8.Valid(b) {
		return errors.New("JSON contains invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var walk func() error
	walk = func() error {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return e
					}
					key := k.(string)
					if seen[key] {
						return fmt.Errorf("duplicate key %q", key)
					}
					seen[key] = true
					if e = walk(); e != nil {
						return e
					}
				}
			case '[':
				for d.More() {
					if e := walk(); e != nil {
						return e
					}
				}
			default:
				return fmt.Errorf("unexpected delimiter")
			}
			_, err = d.Token()
			return err
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	var raw any
	d = json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&raw); err != nil {
		return err
	}
	if err := presence(raw, reflect.TypeOf(dst).Elem()); err != nil {
		return err
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	d.UseNumber()
	return d.Decode(dst)
}

func presence(v any, t reflect.Type) error {
	if t.Kind() == reflect.Pointer {
		if v == nil {
			return nil
		}
		return presence(v, t.Elem())
	}
	if t.Kind() == reflect.Struct {
		// Custom JSON scalars such as time.Time still need the enclosing field
		// presence check, while their own typed decoder validates the encoding.
		if reflect.PointerTo(t).Implements(reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()) {
			return nil
		}
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object")
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := f.Tag.Get("json")
			parts := strings.Split(tag, ",")
			key := parts[0]
			if key == "-" {
				continue
			}
			if key == "" {
				key = f.Name
			}
			val, exists := obj[key]
			optional := strings.Contains(tag, ",omitempty")
			if !exists {
				if !optional {
					return fmt.Errorf("missing field %s", key)
				}
				continue
			}
			if val == nil && f.Type.Kind() != reflect.Pointer && f.Type.Kind() != reflect.Slice && f.Type.Kind() != reflect.Map && f.Type.Kind() != reflect.Interface {
				return fmt.Errorf("null field %s", key)
			}
			if err := presence(val, f.Type); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}
	if t.Kind() == reflect.Slice {
		if arr, ok := v.([]any); ok {
			for _, el := range arr {
				if err := presence(el, t.Elem()); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func Read(path string, dst any) error {
	b, e := ReadBytes(path)
	if e != nil {
		return e
	}
	return Decode(b, dst)
}
func ReadBytes(path string) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	b, e := io.ReadAll(io.LimitReader(f, MaxFile+1))
	closeErr := f.Close()
	if len(b) > MaxFile {
		return nil, errors.Join(e, closeErr, fmt.Errorf("%s exceeds size limit", path))
	}
	return b, errors.Join(e, closeErr)
}
func ReadLines[T any](path string) ([]T, error) {
	b, e := ReadBytes(path)
	if e != nil {
		return nil, e
	}
	s := bufio.NewScanner(bytes.NewReader(b))
	s.Buffer(make([]byte, 4096), MaxRow)
	var rows []T
	for s.Scan() {
		if len(bytes.TrimSpace(s.Bytes())) == 0 {
			continue
		}
		var row T
		if e = Decode(s.Bytes(), &row); e != nil {
			return nil, fmt.Errorf("%s row %d: %w", path, len(rows)+1, e)
		}
		rows = append(rows, row)
	}
	return rows, s.Err()
}

func WriteBytes(path string, b []byte) error {
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	return publishFile(f, b)
}

type durableFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

func publishFile(f durableFile, b []byte) error {
	n, werr := f.Write(b)
	if werr == nil && n != len(b) {
		werr = io.ErrShortWrite
	}
	return errors.Join(werr, f.Sync(), f.Close())
}
func WriteJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return WriteBytes(path, append(b, '\n'))
}
func WriteLines(path string, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return fmt.Errorf("expected slice")
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for i := 0; i < rv.Len(); i++ {
		if e := enc.Encode(rv.Index(i).Interface()); e != nil {
			return e
		}
	}
	return WriteBytes(path, b.Bytes())
}
