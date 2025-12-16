package smp

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var _ Transport = (*testTransport)(nil)

type sendFn func(ctx context.Context, frame SMPFrame) error

type testTransport struct {
	closeFn   func() error
	connectFn func(ctx context.Context) error
	sendFn    func(ctx context.Context, frame SMPFrame) (SMPFrame, error)
}

func newDefaultTestTransport() *testTransport {
	return &testTransport{
		closeFn: func() error {
			return nil
		},
		connectFn: func(ctx context.Context) error {
			return nil
		},
		sendFn: func() func(ctx context.Context, frame SMPFrame) (SMPFrame, error) {
			encoded, _ := EncodeCBOR(FirmwareUploadResponse{
				Off: 1,
			})

			return func(ctx context.Context, frame SMPFrame) (SMPFrame, error) {
				return SMPFrame{
					Header: SMPHeader{
						SequenceNum: frame.Header.SequenceNum,
						DataLength:  uint16(len(encoded)),
					},
					Data: encoded,
				}, nil
			}
		}(),
	}
}

// Close implements [Transport].
func (t *testTransport) Close() error {
	return t.closeFn()
}

// Connect implements [Transport].
func (t *testTransport) Connect(ctx context.Context) error {
	return t.connectFn(ctx)
}

// Send implements [Transport].
func (t *testTransport) Send(ctx context.Context, frame SMPFrame) (SMPFrame, error) {
	return t.sendFn(ctx, frame)
}

func TestUploadWithWindows(t *testing.T) {
	t.Parallel()

	genericError := errors.New("error")

	tests := []struct {
		name        string
		sendFn      sendFn
		expectError error
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
			t.Parallel()

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

				mp, err := DecodeCBOR[map[string]any](frame.Data)
				if err != nil {
					cancel()
					t.Fatalf("decode data: %s", err.Error())
				}

				off := uint32(mp["off"].(uint64))
				copy(uploaded[off:], mp["data"].([]byte))

				encoded, _ := EncodeCBOR(FirmwareUploadResponse{
					Off: off,
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
			err := cl.UploadImageWithWindows(ctx, 3, dataToUpload, chunkSize, func(frame FirmwareUploadRequest) {
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

func TestImgChunkerCorrectness(t *testing.T) {
	// Other tests will verify the upload correctness with parallel chunks.
	// This test will verify that the state of chunker is correct after upload.

	t.Parallel()

	ctx := context.Background()

	transport := newDefaultTestTransport()

	const chunkSize = 1
	const dataSize = 1024
	const maxAllowedWindows = 10

	dataToUpload := make([]byte, dataSize)
	if _, err := rand.Read(dataToUpload); err != nil {
		t.Fatalf("generate data: %s", err.Error())
	}

	chunker := newChunker(transport, maxAllowedWindows, dataToUpload, chunkSize, nil)

	err := chunker.run(ctx)
	if err != nil {
		t.Fatalf("must not error, but got one: %s", err.Error())
	}

	if maxAllowed := chunker.currentAllowedWindows.Load(); maxAllowed != maxAllowedWindows {
		t.Fatalf("want to have %d max windows, but had %d", maxAllowedWindows, maxAllowed)

	}

	if w := chunker.currentWindows.Load(); w != 0 {
		t.Fatalf("current windows must be zero, but was %d", w)
	}

	if semLen := len(chunker.sem); semLen != 0 {
		t.Fatalf("all semaphore spots must be free, but had %d waiting", semLen)
	}
}

func BenchmarkImgUpload(b *testing.B) {
	ctx := context.Background()

	transport := newDefaultTestTransport()

	const chunkSize = 1
	const dataSize = 1024
	const maxAllowedWindows = 10

	dataToUpload := make([]byte, dataSize)
	if _, err := rand.Read(dataToUpload); err != nil {
		b.Fatalf("generate data: %s", err.Error())
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		chunker := newChunker(transport, maxAllowedWindows, dataToUpload, chunkSize, nil)

		if err := chunker.run(ctx); err != nil {
			b.Fatalf("must not error, but got: %s", err.Error())
		}
	}
}
