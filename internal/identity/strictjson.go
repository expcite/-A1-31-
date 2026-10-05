package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// strictDecoder is a json.Decoder that refuses duplicate object keys at every
// nesting level.
//
// encoding/json keeps the LAST value for a duplicated key and reports no error.
// That means "{"revoked":false,"revoked":true}" would parse as revoked=true but
// could be reviewed as revoked=false, and different JSON parsers in different
// languages disagree about which one wins. For a trust registry that is a
// security-relevant ambiguity, so it is rejected outright.
type strictDecoder struct{ *json.Decoder }

func newStrictDecoder(data []byte) *strictDecoder {
	return &strictDecoder{json.NewDecoder(bytes.NewReader(data))}
}

// decodeStrict unmarshals JSON with duplicate keys and trailing data rejected,
// and unknown fields disallowed.
func decodeStrict(data []byte, v any) error {
	if err := newStrictDecoder(data).rejectDuplicates(); err != nil {
		return err
	}
	d := newStrictDecoder(data)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	switch err := d.Decode(&extra); {
	case err == nil:
		return errors.New("trailing data after JSON value")
	case errors.Is(err, io.EOF):
		return nil
	default:
		return err
	}
}

func (d *strictDecoder) rejectDuplicates() error { return d.walk() }

func (d *strictDecoder) walk() error {
	tok, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil // scalar
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			kt, err := d.Token()
			if err != nil {
				return err
			}
			k, ok := kt.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if seen[k] {
				return fmt.Errorf("duplicate key %q", k)
			}
			seen[k] = true
			if err := d.walk(); err != nil {
				return err
			}
		}
		_, err = d.Token() // consume '}'
		return err
	case '[':
		for d.More() {
			if err := d.walk(); err != nil {
				return err
			}
		}
		_, err = d.Token() // consume ']'
		return err
	}
	return nil
}
