package smp

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
)

const DefaultMaxWindowCount = 5

type ImageChunkUploadCallbackFn func(frame FirmwareUploadRequest)

// UploadImageWithWindows will do firmware upload with multiple windows.
//
// It will try to initiate up to `maxWindows` number of requests at once,
// in order to improve throughput.
//
// If no parallel upload is necessary - set `maxWindows` to one.
// In this case chunks will be uploaded sequentially.
func (c *SMPClient) UploadImageWithWindows(ctx context.Context, maxWindows int, data []byte, chunkSize int, cb ImageChunkUploadCallbackFn) error {
	chunker := newChunker(c.transport, maxWindows, data, chunkSize, cb)

	return chunker.run(ctx)
}

type imgChunker struct {
	transport Transport

	data      []byte
	chunkSize int
	cb        ImageChunkUploadCallbackFn

	currentWindows        atomic.Int32
	currentAllowedWindows atomic.Int32

	// sem will have space only for allowed number of windows.
	// It will have capacity for maximum number of windows,
	// but it will have space only for currently allowed number of windows.
	//
	// So to increase number of available windows - just take one item
	// from this channel. To decreasee number of windows - add one item.
	sem          chan struct{}
	chunkOffsets []int
	wg           sync.WaitGroup
}

func newChunker(transport Transport, maxWindows int, data []byte, chunkSize int, cb ImageChunkUploadCallbackFn) *imgChunker {
	chunker := &imgChunker{
		transport: transport,

		data:      data,
		chunkSize: chunkSize,
		cb:        cb,

		sem:          make(chan struct{}, maxWindows),
		chunkOffsets: make([]int, 0, len(data)/int(chunkSize)),
	}

	chunker.currentAllowedWindows.Add(1)

	return chunker
}

func (c *imgChunker) run(ctx context.Context) error {
	// Frequency at which to try and increase window size.
	// Each `windowCheckFreq` chunks it will try to
	// increase number of windows by one up until maximum value.
	const windowCheckFreq = 50

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	dataLen := len(c.data)

	var currOffset int
	for currOffset < dataLen {
		c.chunkOffsets = append(c.chunkOffsets, currOffset)
		currOffset += c.chunkSize
	}
	// Allow only one window to begin with by adding items
	// until only one empty space in chan is available.
	for range cap(c.sem) - 1 {
		c.sem <- struct{}{}
	}

	var err error
	for i, chunkOffset := range c.chunkOffsets {
		if !c.tryUseWindow(ctx) {
			break
		}

		c.wg.Add(1)
		c.currentWindows.Add(1)
		// Chunks are sent sequentially, with waiting for successful send.
		// So it is not needed to track in-flights, or failed requests.
		//
		// Maybe, this can instead be pre-allocated goroutine pool
		// that will then fetch work through channel.
		// But uploading will still be bottle-necked by transport,
		// so it will not matter much.
		go func(i int) {
			defer func() {
				c.currentWindows.Add(-1)
				c.freeWindow()
				c.wg.Done()
			}()

			if iErr := c.sendChunk(ctx, chunkOffset); iErr != nil && err == nil {
				cancel()
				slog.Error("send chunk", "err", iErr.Error())
				// FIXME: This may be racy.
				err = iErr

				return
			}

			// Check if we can increase number of windows
			if currentAllowedWindows := c.currentAllowedWindows.Load(); i%windowCheckFreq == 0 && int(currentAllowedWindows) < cap(c.sem) {
				// As multiple chunks may upload in parallel - it is possible that
				// this update may collide with another update.
				swapped := c.currentAllowedWindows.CompareAndSwap(currentAllowedWindows, currentAllowedWindows+1)
				if swapped {
					// Add one new window spot.
					c.freeWindow()
					if slog.Default().Enabled(ctx, slog.LevelDebug) {
						slog.Debug("increase windows count", "chunkIdx", i, "newVal", currentAllowedWindows+1, "currWindows", c.currentWindows.Load())
					}
				} else {
					slog.Warn("max window count modified in another goroutine")
				}
			}
		}(i)
	}

	c.wg.Wait()

	return err
}

func (c *imgChunker) sendChunk(ctx context.Context, offset int) error {
	dataLen := len(c.data)

	var shaVal []byte
	if offset == 0 {
		shaValArray := sha256.Sum256(c.data)
		shaVal = shaValArray[:]
	}

	nextPtr := min(offset+c.chunkSize, dataLen)

	req := BuildFirmwareUploadRequest(0, uint32(dataLen), uint32(offset), shaVal, c.data[offset:nextPtr], false)
	uploadData, err := EncodeCBOR(req)
	if err != nil {
		return fmt.Errorf("failed to encode firmware upload request: %w", err)
	}

	const maxTries = 3

	var tries int

	for tries < maxTries && ctx.Err() == nil {
		if tries != 0 {
			slog.Warn("re-trying to upload image chunk", "num", tries)
		}

		// Create SMP frame for firmware upload command
		frame := CreateFrame(SMPOpWriteRequest, SMPGroupImage, SMPCmdImageUpload, uploadData)

		// Send the frame
		response, err := c.transport.Send(ctx, frame)
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			// If we got timeout here - try to remove one window, if we have space for it.
			// If not - don't.
			if tries == 0 && c.currentWindows.Load() > 1 {
				// With the value of maxmum number of windows chunker will not
				// try to increase available windows any further,
				// effectively stopping window number increase.
				c.currentAllowedWindows.Store(int32(cap(c.sem)))
				// Consume one window to reduce available number by one.
				c.tryUseWindow(ctx)
			}
			continue
		case err != nil:
			return fmt.Errorf("failed to send firmware upload frame: %w", err)
		}

		// Validate response
		if err := response.ValidateFrame(); err != nil {
			return fmt.Errorf("invalid firmware upload response frame: %w", err)
		}

		// Parse response
		uploadResp, err := DecodeCBOR[FirmwareUploadResponse](response.Data)
		if err != nil {
			return fmt.Errorf("failed to parse firmware upload response: %w", err)
		}

		// Check for errors in response
		if uploadResp.Err.Rc != 0 {
			return fmt.Errorf("firmware upload command failed: group=%d, rc=%d", uploadResp.Err.Group, uploadResp.Err.Rc)
		}

		if c.cb != nil {
			c.cb(req)
		}

		return nil
	}

	if ctx.Err() != nil {
		return fmt.Errorf("context error: %w", ctx.Err())
	}

	return fmt.Errorf("tried to send for %d tries, still failed", maxTries)
}

func (c *imgChunker) tryUseWindow(ctx context.Context) bool {
	// currWinds := c.currentWindows.Load()
	// if currWinds < c.maxWindows {
	// 	return c.currentWindows.CompareAndSwap(currWinds, currWinds+1)
	// }

	select {
	case c.sem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}
func (c *imgChunker) freeWindow() {
	// c.currentWindows.Add(^uint32(0))
	<-c.sem
}
