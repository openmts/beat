package agentupdate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func decodePrivateJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return ErrJournal
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ErrJournal
	}
	return nil
}
