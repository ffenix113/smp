package smp

import (
	"fmt"
	"sync/atomic"
)

// seqNum holds current sequence number to calculate the next one.
//
// FIXME: this variable should have better place.
var seqNum atomic.Uint32

// SMP Protocol Version constants
const (
	SMPVersionLegacy = 0b00
	SMPVersion2      = 0b01
)

// SMP Operation Codes
const (
	SMPOpReadRequest   = 0x00
	SMPOpReadResponse  = 0x01
	SMPOpWriteRequest  = 0x02
	SMPOpWriteResponse = 0x03
)

// Group IDs
const (
	SMPGroupOS          = 0x00
	SMPGroupImage       = 0x01
	SMPGroupEcho        = 0x02
	SMPGroupLog         = 0x04
	SMPGroupTest        = 0x05
	SMPGroupSplitImage  = 0x06
	SMPGroupCrashTest   = 0x07
	SMPGroupShell       = 0x08
	SMPGroupFS          = 0x09
	SMPGroupUserDefined = 0x40
)

// Command IDs for OS Group (Group 0)
const (
	SMPCmdEcho           = 0x00
	SMPCmdConsole        = 0x01
	SMPCmdTaskStats      = 0x02
	SMPCmdMemPoolStats   = 0x03
	SMPCmdDateTime       = 0x04
	SMPCmdReset          = 0x05
	SMPCmdMCUMgrParams   = 0x06
	SMPCmdOSInfo         = 0x07
	SMPCmdBootloaderInfo = 0x08
)

// Command IDs for Image Group (Group 1)
const (
	SMPCmdImageState  = 0x00
	SMPCmdImageUpload = 0x01
	SMPCmdFile        = 0x02
	SMPCmdCorelist    = 0x03
	SMPCmdCoreload    = 0x04
	SMPCmdImageErase  = 0x05
)

// Error codes
const (
	Success = 0x00
)

// SMP Frame Header
type SMPHeader struct {
	Version     uint8
	Op          uint8
	Flags       uint8
	DataLength  uint16
	GroupID     uint8
	SequenceNum uint8
	CommandID   uint8
}

// SMP Frame represents the complete SMP message
type SMPFrame struct {
	Header SMPHeader
	Data   []byte
}

// SMP Client encapsulates the SMP communication
type SMPClient struct {
	transport Transport
}

// NewSMPClient creates a new SMP client with the given transport
func NewSMPClient(transport Transport) *SMPClient {
	return &SMPClient{
		transport: transport,
	}
}

func NextSeqNum() uint8 {
	// This can return 0 on wrap-around,
	// but it does not look like it should be a problem.
	//
	// https://docs.zephyrproject.org/latest/services/device_mgmt/smp_protocol.html#frame-the-envelope
	return uint8(seqNum.Add(1) % 0xff)
}

// CreateFrame creates a new SMP frame with the specified parameters.
//
// Sequence number is generated atomically, it is not possible to change
// how it is generated currently.
//
// But it is always possible to change its value on the frame before sending it.
func CreateFrame(op uint8, groupID uint8, commandID uint8, data []byte) SMPFrame {
	return SMPFrame{
		Header: SMPHeader{
			Version:     SMPVersion2,
			Op:          op,
			Flags:       0x00,
			DataLength:  uint16(len(data)),
			GroupID:     groupID,
			SequenceNum: NextSeqNum(),
			CommandID:   commandID,
		},
		Data: data,
	}
}

// ValidateFrame validates the SMP frame
func (f *SMPFrame) ValidateFrame() error {
	// Check for nil frame
	if f == nil {
		return fmt.Errorf("frame cannot be nil")
	}

	// Basic validation
	if f.Header.DataLength != uint16(len(f.Data)) {
		return fmt.Errorf("data length mismatch: header=%d, actual=%d", f.Header.DataLength, len(f.Data))
	}

	// Check version
	if f.Header.Version != SMPVersionLegacy && f.Header.Version != SMPVersion2 {
		return fmt.Errorf("invalid version: %d", f.Header.Version)
	}

	return nil
}
