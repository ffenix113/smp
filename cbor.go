package smp

import (
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

// CBOR utilities for SMP protocol

// CBOR encodes the data payload for SMP commands
func EncodeCBOR(data any) ([]byte, error) {
	// Default CBOR encoding
	encoded, err := cbor.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to encode CBOR: %v", err)
	}

	return encoded, nil
}

// CBOR decodes the data payload from SMP responses
func DecodeCBOR[T any](data []byte) (T, error) {
	var val T
	if err := cbor.Unmarshal(data, &val); err != nil {
		return val, fmt.Errorf("failed to decode CBOR: %v", err)
	}

	return val, nil
}
