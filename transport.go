package simple_smp

import (
	"context"
	"errors"
	"net/url"
)

var ErrWaitTimeout = errors.New("wait timeout")

// Transport interface defines the contract for different transport implementations
type Transport interface {
	Connect(ctx context.Context, params url.Values) error
	// Send will send frame and receive response, synchronously.
	// Even if underlying transport is async - this method
	// will wait for response to be received.
	Send(ctx context.Context, frame SMPFrame) (SMPFrame, error)
	Close() error
}
