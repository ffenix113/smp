package simple_smp

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
)

// UploadFirmware uploads a new firmware image to the device
func (c *SMPClient) UploadFirmware(ctx context.Context, data []byte, chunkSize uint32, cb func(offset uint32)) error {
	// Build CBOR payload for firmware upload request

	dataSha := sha256.Sum256(data)

	uintSize := uint32(len(data))

	var offset uint32
	for offset < uint32(len(data)) {
		nextPtr := min(offset+uint32(chunkSize), uintSize)
		req := BuildFirmwareUploadRequest(0, uintSize, offset, dataSha[:], data[offset:nextPtr], false)
		uploadData, err := EncodeCBOR(req)
		if err != nil {
			return fmt.Errorf("failed to encode firmware upload request: %v", err)
		}

		// Create SMP frame for firmware upload command
		frame := c.CreateFrame(SMPOpWriteRequest, SMPGroupImage, SMPCmdImageUpload, uploadData)

		// Send the frame
		response, err := c.transport.Send(ctx, frame)
		if err != nil {
			return fmt.Errorf("failed to send firmware upload frame: %v", err)
		}

		// Validate response
		if err := response.ValidateFrame(); err != nil {
			return fmt.Errorf("invalid firmware upload response frame: %v", err)
		}

		// Parse response
		uploadResp, err := ParseFirmwareUploadResponse(response.Data)
		if err != nil {
			return fmt.Errorf("failed to parse firmware upload response: %v", err)
		}

		// Check for errors in response
		if uploadResp.Err != nil || uploadResp.Off == nil {
			return fmt.Errorf("firmware upload command failed: group=%d, rc=%d", uploadResp.Err.Group, uploadResp.Err.Rc)
		}

		// Check if upload was successful
		// if uploadResp.Off == nil || *uploadResp.Off != offset+uint32(len(data)) {
		// 	return fmt.Errorf("firmware upload did not complete correctly")
		// }

		offset = *uploadResp.Off

		if cb != nil {
			cb(offset)
		}
	}

	return nil
}

func (c *SMPClient) UploadFirmware2(ctx context.Context, data []byte, chunkSize uint32, cb func(frame FirmwareUploadRequest)) error {
	chunker := newChunker(c, data, chunkSize)

	return chunker.run(ctx, cb)
}

type imgChunker struct {
	client *SMPClient

	data      []byte
	chunkSize uint32

	currentWindows atomic.Uint32

	sema         chan struct{}
	maxWindows   uint32
	chunkOffsets []uint32
	wg           sync.WaitGroup
}

func newChunker(client *SMPClient, data []byte, chunkSize uint32) *imgChunker {
	return &imgChunker{
		client: client,

		data:      data,
		chunkSize: chunkSize,

		sema:         make(chan struct{}, 16),
		maxWindows:   1,
		chunkOffsets: make([]uint32, 0, len(data)/int(chunkSize)),
	}
}

func (c *imgChunker) run(ctx context.Context, cb func(frame FirmwareUploadRequest)) error {
	dataLen := uint32(len(c.data))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var currOffset uint32
	for currOffset < dataLen {
		c.chunkOffsets = append(c.chunkOffsets, currOffset)
		currOffset += c.chunkSize
	}

	for range cap(c.sema) - 1 {
		c.sema <- struct{}{}
	}

	var err error
	for i, chunkOffset := range c.chunkOffsets {
		slog.Info("wait for sema")
		if !c.tryUseWindow(ctx) {
			break
		}

		c.wg.Add(1)

		go func(i int) {
			defer func() {
				slog.Info("freed sema")
				c.freeWindow()
			}()

			if iErr := c.sendChunk(ctx, chunkOffset, cb); iErr != nil && err == nil {
				cancel()
				slog.Error("send chunk", "err", iErr.Error())
				err = iErr
			}

			if err != nil {
				return
			}

			if i%50 == 0 && c.maxWindows < 4 {
				c.freeWindow()
				c.maxWindows++
				slog.Info("increase windows count", "i", i, "newVal", c.maxWindows, "currWindows", c.currentWindows.Load())
			}
		}(i)
	}

	slog.Info("wait for routines")
	c.wg.Wait()

	return err
}

func (c *imgChunker) sendChunk(ctx context.Context, offset uint32, cb func(frame FirmwareUploadRequest)) error {
	defer c.wg.Done()

	uintDataLen := uint32(len(c.data))

	var shaVal []byte
	if offset == 0 {
		shaValArray := sha256.Sum256(c.data)
		shaVal = shaValArray[:]

		// chunkSize -= sha256.Size
	}

	nextPtr := min(offset+c.chunkSize, uintDataLen)

	req := BuildFirmwareUploadRequest(0, uintDataLen, offset, shaVal, c.data[offset:nextPtr], false)
	uploadData, err := EncodeCBOR(req)
	if err != nil {
		return fmt.Errorf("failed to encode firmware upload request: %v", err)
	}

	const maxTries = 3
	var tries int

	for tries < maxTries && ctx.Err() == nil {
		if tries != 0 {
			slog.Warn("not zero try", "num", tries)
		}

		// Create SMP frame for firmware upload command
		frame := c.client.CreateFrame(SMPOpWriteRequest, SMPGroupImage, SMPCmdImageUpload, uploadData)

		// Send the frame
		response, err := c.client.transport.Send(ctx, frame)
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			if tries == 0 {
				c.tryUseWindow(ctx)
			}
			continue
		case err != nil:
			return fmt.Errorf("failed to send firmware upload frame: %v", err)
		}

		// Validate response
		if err := response.ValidateFrame(); err != nil {
			return fmt.Errorf("invalid firmware upload response frame: %v", err)
		}

		// Parse response
		uploadResp, err := ParseFirmwareUploadResponse(response.Data)
		if err != nil {
			return fmt.Errorf("failed to parse firmware upload response: %v", err)
		}

		// Check for errors in response
		if uploadResp.Err != nil {
			return fmt.Errorf("firmware upload command failed: group=%d, rc=%d", uploadResp.Err.Group, uploadResp.Err.Rc)
		}

		cb(req)

		return nil
	}

	return fmt.Errorf("tried to send for %d tries, still failed", maxTries)
}

func (c *imgChunker) tryUseWindow(ctx context.Context) bool {
	// currWinds := c.currentWindows.Load()
	// if currWinds < c.maxWindows {
	// 	return c.currentWindows.CompareAndSwap(currWinds, currWinds+1)
	// }

	select {
	case c.sema <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}
func (c *imgChunker) freeWindow() {
	// c.currentWindows.Add(^uint32(0))
	<-c.sema
}
