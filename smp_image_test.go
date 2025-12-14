package simple_smp

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var _ Transport = (*testTransport)(nil)

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
	tests := []struct {
		name                        string
		dataSize                    int
		chunkSize                   uint32
		expectError                 bool
		transportErrorMsg           string
		simulateRetryableError      bool
		simulateContextCancellation bool
		cancelAfterChunk            int
	}{
		{
			name:      "Normal upload with 384 byte chunks",
			dataSize:  128 * 1024,
			chunkSize: 384,
		},
		{
			name:              "Transport error on first chunk",
			dataSize:          1024,
			chunkSize:         1,
			expectError:       true,
			transportErrorMsg: "transport error: connection timeout",
		},
		{
			name:      "Success with 1 byte chunks",
			dataSize:  1024,
			chunkSize: 1,
		},
		{
			name:      "Large upload with window scaling (5000+ chunks)",
			dataSize:  2 * 1024 * 1024, // 2MB to ensure we get 5000+ chunks with small chunk size
			chunkSize: 400,
		},
		{
			name:                        "Context cancellation during upload",
			dataSize:                    100 * 1024,
			chunkSize:                   100,
			expectError:                 true,
			simulateContextCancellation: true,
			cancelAfterChunk:            50,
		},
		{
			name:                   "Retryable error with eventual success",
			dataSize:               1024,
			chunkSize:              512,
			simulateRetryableError: true,
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
			var chunkCounter atomic.Int32

			uploaded := make([]byte, tt.dataSize)

			transport.sendFn = func(ctx context.Context, frame SMPFrame) (SMPFrame, error) {
				if tt.transportErrorMsg != "" {
					// Simulate a transport error on the first chunk
					return SMPFrame{}, errors.New(tt.transportErrorMsg)
				}

				if tt.simulateContextCancellation {
					currentChunk := chunkCounter.Add(1)
					if currentChunk >= int32(tt.cancelAfterChunk) {
						cancel()
						return SMPFrame{}, context.Canceled
					}
				}

				if tt.simulateRetryableError {
					// Simulate a retryable error (like deadline exceeded) for the first few chunks
					currentChunk := chunkCounter.Add(1)
					if currentChunk <= 2 {
						return SMPFrame{}, context.DeadlineExceeded
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

			dataToUpload := make([]byte, tt.dataSize)
			if _, err := rand.Read(dataToUpload); err != nil {
				t.Fatalf("generate data: %s", err.Error())
			}

			cl := NewSMPClient(transport)
			err := cl.UploadFirmware2(ctx, dataToUpload, tt.chunkSize, func(frame FirmwareUploadRequest) {
				if tt.transportErrorMsg != "" {
					t.Fatalf("should not be called on transport error")
				}
				uploadedSize += uint32(len(frame.Data))
				uploadedChunks++
			})

			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error but got none")
				}

				if tt.simulateContextCancellation {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("expected context.Canceled error, got: %v", err)
					}
				} else if !errors.Is(err, context.Canceled) {
					t.Logf("got error: %v", err)
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
				start, end := int(tt.chunkSize)*i, min(int(tt.chunkSize)*(i+1), len(dataToUpload))
				toUpload, uploaded := dataToUpload[start:end], uploaded[start:end]
				if !bytes.Equal(toUpload, uploaded) {
					t.Fatalf("uploaded data differ on chunk %d: \n%v != \n%v", i, toUpload, uploaded)
				}
			}
		})
	}
}
