package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func (report *AcceleratorReport) UnmarshalJSON(data []byte) error {
	type wire AcceleratorReport
	var value wire
	if err := decodeAcceleratorJSON(data, &value); err != nil {
		return err
	}
	*report = AcceleratorReport(value)
	return nil
}

func decodeAcceleratorJSON(data []byte, value any) error {
	if len(data) > MaximumAcceleratorSnapshotBytes {
		return ErrAgentReportInvalid
	}
	tokens := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueAcceleratorJSON(tokens, 0); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return ErrAgentReportInvalid
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ErrAgentReportInvalid
	}
	return nil
}

func uniqueAcceleratorJSON(decoder *json.Decoder, depth int) error {
	if depth > 12 {
		return ErrAgentReportInvalid
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrAgentReportInvalid
	}
	switch token {
	case json.Delim('{'):
		return uniqueAcceleratorObject(decoder, depth)
	case json.Delim('['):
		for decoder.More() {
			if err := uniqueAcceleratorJSON(decoder, depth+1); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return ErrAgentReportInvalid
		}
	}
	return nil
}

func uniqueAcceleratorObject(decoder *json.Decoder, depth int) error {
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return ErrAgentReportInvalid
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return ErrAgentReportInvalid
		}
		seen[key] = true
		if err := uniqueAcceleratorJSON(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	if err != nil {
		return ErrAgentReportInvalid
	}
	return nil
}
