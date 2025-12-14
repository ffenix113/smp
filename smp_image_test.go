package simple_smp

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var _ Transport = (*testTransport)(nil)

type sendFn func(ctx context.Context, frame SMPFrame) error

type testTransport struct {
	closeFn   func() error
	connectFn func(ctx context.Context, params url.Values) error
	sendFn    func(ctx context.Context, frame SMPFrame) (SMPFrame, error)
}

func newDefaultTestTransport() *testTransport {
	return &testTransport{
		closeFn: func() error {
			return nil
		},
		connectFn: func(ctx context.Context, params url.Values) error {
			return nil
		},
		sendFn: func(ctx context.Context, frame SMPFrame) (SMPFrame, error) {
			return SMPFrame{}, nil
		},
	}
}

// Close implements [Transport].
func (t *testTransport) Close() error {
	return t.closeFn()
}

// Connect implements [Transport].
func (t *testTransport) Connect(ctx context.Context, params url.Values) error {
	return t.connectFn(ctx, params)
}

// Send implements [Transport].
func (t *testTransport) Send(ctx context.Context, frame SMPFrame) (SMPFrame, error) {
	return t.sendFn(ctx, frame)
}

func TestImgChunker(t *testing.T) {

	genericError := errors.New("error")

	tests := []struct {
		name        string
		sendFn      sendFn
		expectError error
		// transportErrorMsg string
	}{
		{
			name: "Normal upload",
		},
		{
			name:        "Transport error on first chunk",
			expectError: genericError,
			sendFn: func(ctx context.Context, frame SMPFrame) error {
				return genericError
			},
		},
		{
			name:        "Context cancellation during upload",
			expectError: context.Canceled,
			sendFn: func() sendFn {
				var failAfter uint32 = 51
				var currentChunk atomic.Uint32
				return func(ctx context.Context, req SMPFrame) error {
					if currentChunk.Add(1) == failAfter {
						return context.Canceled
					}

					return nil
				}
			}(),
		},
		{
			name: "Retryable error with eventual success",
			sendFn: func() sendFn {
				var wasTimedout atomic.Bool
				return func(ctx context.Context, frame SMPFrame) error {
					if wasTimedout.CompareAndSwap(false, true) {
						return context.DeadlineExceeded
					}

					return nil
				}
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			t.Cleanup(cancel)

			transport := newDefaultTestTransport()

			var uploadedMu sync.Mutex
			var uploadedChunks int
			var uploadedSize uint32

			const chunkSize = 1
			const dataSize = 1024
			uploaded := make([]byte, dataSize)

			dataToUpload := make([]byte, dataSize)
			if _, err := rand.Read(dataToUpload); err != nil {
				t.Fatalf("generate data: %s", err.Error())
			}

			transport.sendFn = func(ctx context.Context, frame SMPFrame) (SMPFrame, error) {
				if sender := tt.sendFn; sender != nil {
					if err := sender(ctx, frame); err != nil {
						return SMPFrame{}, err
					}
				}

				uploadedMu.Lock()
				defer uploadedMu.Unlock()

				mp := make(map[string]any)
				if err := DecodeCBOR(frame.Data, &mp); err != nil {
					cancel()
					t.Fatalf("decode data: %s", err.Error())
				}

				off := uint32(mp["off"].(uint64))
				copy(uploaded[off:], mp["data"].([]byte))

				encoded, _ := EncodeCBOR(FirmwareUploadResponse{
					Off: &off,
				})

				return SMPFrame{
					Header: SMPHeader{
						SequenceNum: frame.Header.SequenceNum,
						DataLength:  uint16(len(encoded)),
					},
					Data: encoded,
				}, nil
			}

			cl := NewSMPClient(transport)
			err := cl.UploadFirmware2(ctx, dataToUpload, chunkSize, func(frame FirmwareUploadRequest) {
				uploadedSize += uint32(len(frame.Data))
				uploadedChunks++
			})

			if tt.expectError != nil {
				if err == nil {
					t.Fatalf("expected error but got none")
				}

				if !errors.Is(err, tt.expectError) {
					t.Logf("wrong error: %s, want: %s", err.Error(), tt.expectError.Error())
				}

				return
			}

			if err != nil {
				t.Fatalf("upload err: %s", err.Error())
			}

			if int(uploadedSize) != len(dataToUpload) {
				t.Fatalf("uploaded size different: %d != %d", len(dataToUpload), uploadedSize)
			}

			for i := 0; i < uploadedChunks; i++ {
				start, end := int(chunkSize)*i, min(int(chunkSize)*(i+1), len(dataToUpload))
				toUpload, uploaded := dataToUpload[start:end], uploaded[start:end]
				if !bytes.Equal(toUpload, uploaded) {
					t.Fatalf("uploaded data differ on chunk %d: \n%v != \n%v", i, toUpload, uploaded)
				}
			}
		})
	}
}
