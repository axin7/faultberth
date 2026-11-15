// Package envelope parses the public Sentry envelope wire format.
package envelope

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Envelope retains unknown headers and items so callers can decide how to handle them.
type Envelope struct {
	Headers json.RawMessage
	Items   []Item
}

type Item struct {
	Headers json.RawMessage
	Type    string
	Payload []byte
}

type itemHeader struct {
	Type   string `json:"type"`
	Length *int64 `json:"length"`
}

// Parse rejects malformed input and bounds both input and retained payload memory.
func Parse(r io.Reader, maxBytes int64) (Envelope, error) {
	if maxBytes <= 0 {
		return Envelope{}, errors.New("invalid envelope size limit")
	}
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return Envelope{}, fmt.Errorf("read envelope: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return Envelope{}, errors.New("envelope exceeds size limit")
	}
	reader := bufio.NewReaderSize(bytes.NewReader(data), len(data)+1)
	headers, err := readHeader(reader)
	if err != nil {
		return Envelope{}, fmt.Errorf("envelope header: %w", err)
	}
	result := Envelope{Headers: headers}
	for reader.Buffered() > 0 {
		item, err := readItem(reader)
		if err != nil {
			return Envelope{}, err
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func readHeader(r *bufio.Reader) (json.RawMessage, error) {
	line, err := r.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	line = bytes.TrimSuffix(line, []byte{'\n'})
	var object map[string]json.RawMessage
	if len(line) == 0 || json.Unmarshal(line, &object) != nil || object == nil {
		return nil, errors.New("expected a JSON object")
	}
	return json.RawMessage(line), nil
}

func readItem(r *bufio.Reader) (Item, error) {
	headers, err := readHeader(r)
	if err != nil {
		return Item{}, fmt.Errorf("item header: %w", err)
	}
	var header itemHeader
	if err := json.Unmarshal(headers, &header); err != nil || header.Type == "" {
		return Item{}, errors.New("invalid item type or length")
	}
	payload, err := readPayload(r, header.Length)
	if err != nil {
		return Item{}, fmt.Errorf("item payload: %w", err)
	}
	return Item{Headers: headers, Type: header.Type, Payload: payload}, nil
}

func readPayload(r *bufio.Reader, length *int64) ([]byte, error) {
	if length == nil {
		payload, err := r.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		return bytes.TrimSuffix(payload, []byte{'\n'}), nil
	}
	if *length < 0 || *length > int64(r.Buffered()) {
		return nil, errors.New("invalid or truncated length")
	}
	payload := make([]byte, int(*length))
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	if r.Buffered() > 0 {
		end, _ := r.ReadByte()
		if end != '\n' {
			return nil, errors.New("length-prefixed item missing newline")
		}
	}
	return payload, nil
}
