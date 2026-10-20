package acceleratorcollect

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func boundedJSONObject(raw []byte, maximum int) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrInvalidOutput
	}
	result := map[string]json.RawMessage{}
	for decoder.More() {
		if len(result) >= maximum {
			return nil, ErrInvalidOutput
		}
		if err := decodeObjectField(decoder, result); err != nil {
			return nil, err
		}
	}
	if _, err = decoder.Token(); err != nil {
		return nil, ErrInvalidOutput
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidOutput
	}
	return result, nil
}

func decodeObjectField(decoder *json.Decoder, result map[string]json.RawMessage) error {
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalidOutput
	}
	key, ok := token.(string)
	if !ok {
		return ErrInvalidOutput
	}
	if _, duplicate := result[key]; duplicate {
		return ErrInvalidOutput
	}
	var value json.RawMessage
	if err = decoder.Decode(&value); err != nil {
		return ErrInvalidOutput
	}
	result[key] = value
	return nil
}
