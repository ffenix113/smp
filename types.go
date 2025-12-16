package smp

import "fmt"

// ResetRequest represents the CBOR data for a reset command
type ResetRequest struct {
	// While SMP protocol defines this as int,
	// MCUMgr accepts it as boolean.
	Force bool `cbor:"force,omitempty"`
}

// ResetResponse represents the CBOR data for a reset response
type ResetResponse struct {
	Err *ErrorResponse `cbor:"err,omitempty"` // Optional error response
}

// ErrorResponse represents the error response in SMP v2
type ErrorResponse struct {
	Group uint8 `cbor:"group"`
	Rc    uint8 `cbor:"rc"`
}

// FirmwareUploadRequest represents the CBOR data for firmware upload
type FirmwareUploadRequest struct {
	Image   uint32 `cbor:"image,omitempty"`
	Len     uint32 `cbor:"len,omitempty"`
	Off     uint32 `cbor:"off"`
	SHA     []byte `cbor:"sha,omitempty"`
	Data    []byte `cbor:"data"`
	Upgrade bool   `cbor:"upgrade,omitempty"`
}

// FirmwareUploadResponse represents the CBOR data for firmware upload response
type FirmwareUploadResponse struct {
	Off   uint32        `cbor:"off,omitempty"`
	Match bool          `cbor:"match,omitempty"`
	Err   ErrorResponse `cbor:"err,omitempty"` // Optional error response
}

// ImageStateRequest represents the CBOR data for image state request
type ImageStateRequest struct {
	// Empty request
}

// ImageStateResponse represents the CBOR data for image state response
type ImageStateResponse struct {
	Images      []ImageInfo    `cbor:"images"`
	SplitStatus *int           `cbor:"splitStatus,omitempty"`
	Err         *ErrorResponse `cbor:"err,omitempty"` // Optional error response
}

// ImageInfo represents information about a specific image
type ImageInfo struct {
	Image     *uint32 `cbor:"image,omitempty"`
	Slot      uint32  `cbor:"slot"`
	Version   string  `cbor:"version"`
	Hash      []byte  `cbor:"hash,omitempty"`
	Bootable  *bool   `cbor:"bootable,omitempty"`
	Pending   *bool   `cbor:"pending,omitempty"`
	Confirmed *bool   `cbor:"confirmed,omitempty"`
	Active    *bool   `cbor:"active,omitempty"`
	Permanent *bool   `cbor:"permanent,omitempty"`
}

// ImageEraseRequest represents the CBOR data for image erase request
type ImageEraseRequest struct {
	Slot *uint32 `cbor:"slot,omitempty"`
}

// ImageEraseResponse represents the CBOR data for image erase response
type ImageEraseResponse struct {
	Err *ErrorResponse `cbor:"err,omitempty"` // Optional error response
}

// BuildResetRequest creates a CBOR-encoded reset request
func BuildResetRequest(force bool) ResetRequest {
	return ResetRequest{Force: force}
}

// BuildFirmwareUploadRequest creates a CBOR-encoded firmware upload request
func BuildFirmwareUploadRequest(image uint32, length uint32, offset uint32, sha256 []byte, data []byte, upgrade bool) FirmwareUploadRequest {
	req := FirmwareUploadRequest{
		Off:  offset,
		Data: data,
	}

	if offset == 0 {
		req.Image = image
		req.Len = length
		req.SHA = sha256
		req.Upgrade = upgrade
	}

	return req
}

// BuildImageStateRequest creates a CBOR-encoded image state request
func BuildImageStateRequest() ImageStateRequest {
	return ImageStateRequest{}
}

// BuildImageEraseRequest creates a CBOR-encoded image erase request
func BuildImageEraseRequest(slot *uint32) ImageEraseRequest {
	return ImageEraseRequest{Slot: slot}
}

// FrameToSMPFrame converts a raw frame data to SMPFrame structure
func FrameToSMPFrame(frameData []byte) (SMPFrame, error) {
	if len(frameData) < 8 {
		return SMPFrame{}, fmt.Errorf("frame too small, minimum 8 bytes required")
	}

	// Extract header (first 8 bytes)
	headerBytes := frameData[:8]
	dataBytes := frameData[8:]

	// Parse header fields assuming big-endian
	header := SMPHeader{
		Version:     (headerBytes[0] >> 3) & 0x03,
		Op:          headerBytes[0] & 0x07,
		Flags:       headerBytes[1],
		DataLength:  uint16(headerBytes[2])<<8 | uint16(headerBytes[3]),
		GroupID:     headerBytes[5],
		SequenceNum: headerBytes[6],
		CommandID:   headerBytes[7],
	}

	// Validate data length
	if int(header.DataLength) != len(dataBytes) {
		return SMPFrame{}, fmt.Errorf("data length mismatch: header: %d, actual: %d", header.DataLength, len(dataBytes))
	}

	return SMPFrame{
		Header: header,
		Data:   dataBytes,
	}, nil
}

// SMPFrameToFrame converts SMPFrame to raw frame data
func SMPFrameToFrame(frame SMPFrame) ([]byte, error) {
	// Create header buffer
	header := make([]byte, 8, 8+len(frame.Data))

	// Pack header fields into bytes
	header[0] = (frame.Header.Version << 3) | frame.Header.Op
	header[1] = frame.Header.Flags
	header[2] = byte(frame.Header.DataLength >> 8)
	header[3] = byte(frame.Header.DataLength & 0xFF)
	// header[4] is groupID, but it will always be empty
	// for groups without user-defined ones.
	header[5] = frame.Header.GroupID
	header[6] = frame.Header.SequenceNum
	header[7] = frame.Header.CommandID

	// Combine header and data
	result := append(header, frame.Data...)

	return result, nil
}
