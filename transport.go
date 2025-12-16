package smp

import (
	"context"
	"errors"
)

var ErrWaitTimeout = errors.New("wait timeout")

// Transport interface defines the contract for different transport implementations
type Transport interface {
	// Connect only does actions necessary to connect to already specified device.
	//
	// Device connection properties are specified in the constructor of the transport.
	Connect(ctx context.Context) error
	// Send will send frame and receive response, synchronously.
	// Even if underlying transport is async - this method
	// will wait for response to be received.
	Send(ctx context.Context, frame SMPFrame) (SMPFrame, error)
	Close() error
}
