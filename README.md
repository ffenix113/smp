# Simple Management Protocol (SMP) Library

A Go implementation of the Simple Management Protocol (SMP) for device management and firmware updates.

## Overview

This library provides a complete implementation of the SMP protocol as specified in the Zephyr RTOS documentation. It includes support for:

- Device reset with optional force parameter
- Firmware image upload
- Frame encoding/decoding
- CBOR serialization
- Transport abstraction layer

## Features

- Full SMP protocol implementation
- CBOR encoding/decoding using `github.com/fxamacker/cbor/v2`
- Transport abstraction for different communication channels
- Comprehensive error handling
- Extensive test coverage
- Thread-safe operations

## Installation

```bash
go get github.com/yourusername/simple_smp
```

## Quick Start

### Basic Usage

```go
package main

import (
	"fmt"
	"log"
	
	"github.com/yourusername/simple_smp"
)

// MockTransport implements the Transport interface for demonstration
type MockTransport struct {
	frames []*simple_smp.SMPFrame
}

func (m *MockTransport) Send(frame *simple_smp.SMPFrame) error {
	m.frames = append(m.frames, frame)
	return nil
}

func (m *MockTransport) Receive() (*simple_smp.SMPFrame, error) {
	if len(m.frames) == 0 {
		return nil, fmt.Errorf("no frames available")
	}
	frame := m.frames[0]
	m.frames = m.frames[1:]
	return frame, nil
}

func (m *MockTransport) Close() error {
	return nil
}

func main() {
	// Create a mock transport
	transport := &MockTransport{}
	
	// Create SMP client
	client := simple_smp.NewSMPClient(transport)
	
	// Reset the device
	err := client.ResetDevice(false)
	if err != nil {
		log.Fatalf("Failed to reset device: %v", err)
	}
	
	fmt.Println("Device reset successful")
	
	// Upload firmware
	firmwareData := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	err = client.UploadFirmware(firmwareData, 0, uint32(len(firmwareData)), nil)
	if err != nil {
		log.Fatalf("Failed to upload firmware: %v", err)
	}
	
	fmt.Println("Firmware upload successful")
}
```

### Frame Construction

```go
package main

import (
	"fmt"
	"log"
	
	"github.com/yourusername/simple_smp"
)

func main() {
	// Create a new client with any transport (for demonstration)
	transport := &MockTransport{}
	client := simple_smp.NewSMPClient(transport)
	
	// Create a custom frame
	data := []byte{0x01, 0x02, 0x03}
	frame := client.CreateFrame(
		simple_smp.SMPOpWriteRequest,
		simple_smp.SMPGroupOS,
		simple_smp.SMPCmdEcho,
		data,
	)
	
	// Validate the frame
	err := frame.ValidateFrame()
	if err != nil {
		log.Fatalf("Invalid frame: %v", err)
	}
	
	fmt.Printf("Created frame: %+v\n", frame)
}
```

### CBOR Operations

```go
package main

import (
	"fmt"
	"log"
	
	"github.com/yourusername/simple_smp"
)

func main() {
	// Build a reset request with force parameter
	data, err := simple_smp.BuildResetRequest(true)
	if err != nil {
		log.Fatalf("Failed to build reset request: %v", err)
	}
	
	fmt.Printf("Reset request CBOR: %x\n", data)
	
	// Parse a reset response
	response := []byte{0xa2, 0xa4, 0x65, 0x72, 0x72, 0xa5, 0x67, 0x72, 0x6f, 0x75, 0x70, 0x00, 0x61, 0x72, 0x63, 0x00}
	resetResp, err := simple_smp.ParseResetResponse(response)
	if err != nil {
		log.Fatalf("Failed to parse reset response: %v", err)
	}
	
	if resetResp.Err != nil {
		fmt.Printf("Reset error: group=%d, rc=%d\n", resetResp.Err.Group, resetResp.Err.Rc)
	} else {
		fmt.Println("Reset successful")
	}
}
```

## API Reference

### Core Types

#### `SMPClient`
Main client for SMP operations.

```go
type SMPClient struct {
    transport Transport
    seqNum    uint8
}
```

#### `Transport`
Interface for different transport implementations.

```go
type Transport interface {
    Send(frame *SMPFrame) error
    Receive() (*SMPFrame, error)
    Close() error
}
```

#### `SMPFrame`
Represents a complete SMP message.

```go
type SMPFrame struct {
    Header SMPHeader
    Data   []byte
}
```

### Functions

#### Client Operations

- `NewSMPClient(transport Transport) *SMPClient`
  - Creates a new SMP client with the specified transport.

- `ResetDevice(force bool) error`
  - Sends a device reset command.
  - `force`: If true, forces a reset even if device is busy.

- `UploadFirmware(data []byte, offset uint32, length uint32, sha256 []byte) error`
  - Uploads firmware to the device.
  - `data`: Firmware data chunk.
  - `offset`: Offset in the firmware image.
  - `length`: Total length of the firmware image.
  - `sha256`: Optional SHA256 hash of the complete image.

#### Frame Operations

- `CreateFrame(op uint8, groupID uint8, commandID uint8, data []byte) *SMPFrame`
  - Creates a new SMP frame with the specified parameters.

- `ValidateFrame() error`
  - Validates the SMP frame structure.

#### CBOR Operations

- `BuildResetRequest(force bool) ([]byte, error)`
  - Encodes a reset request into CBOR format.

- `BuildFirmwareUploadRequest(...) ([]byte, error)`
  - Encodes a firmware upload request into CBOR format.

- `ParseResetResponse(data []byte) (*ResetResponse, error)`
  - Decodes a reset response from CBOR format.

- `ParseFirmwareUploadResponse(data []byte) (*FirmwareUploadResponse, error)`
  - Decodes a firmware upload response from CBOR format.

### Constants

#### Protocol Constants
- `SMPVersionLegacy = 0b00`
- `SMPVersion2 = 0b01`

#### Operation Codes
- `SMPOpWriteRequest = 0x00`
- `SMPOpReadRequest = 0x01`
- `SMPOpWriteResponse = 0x02`
- `SMPOpReadResponse = 0x03`

#### Group IDs
- `SMPGroupOS = 0x00`
- `SMPGroupImage = 0x01`
- `SMPGroupEcho = 0x02`
- `SMPGroupLog = 0x04`
- `SMPGroupTest = 0x05`
- `SMPGroupSplitImage = 0x06`
- `SMPGroupCrashTest = 0x07`
- `SMPGroupShell = 0x08`
- `SMPGroupFS = 0x09`
- `SMPGroupUserDefined = 0x40`

#### Command IDs
- `SMPCmdReset = 0x05` (OS Group)
- `SMPCmdImageUpload = 0x01` (Image Group)

## Error Handling

The library provides comprehensive error handling:

1. **Transport errors**: Errors during send/receive operations
2. **Frame validation errors**: Invalid frame structures
3. **CBOR errors**: Encoding/decoding failures
4. **Protocol errors**: SMP-specific error responses

```go
err := client.ResetDevice(true)
if err != nil {
    // Handle error
    switch {
    case errors.Is(err, transport.ErrTimeout):
        // Handle transport timeout
    case errors.Is(err, &FrameValidationError{}):
        // Handle invalid frame
    default:
        // Handle other errors
    }
}
```

## Transport Implementation Examples

### Serial Transport

```go
type SerialTransport struct {
    port *serial.Port
}

func (s *SerialTransport) Send(frame *simple_smp.SMPFrame) error {
    data, err := simple_smp.SMPFrameToFrame(frame)
    if err != nil {
        return err
    }
    
    return s.port.Write(data)
}

func (s *SerialTransport) Receive() (*simple_smp.SMPFrame, error) {
    // Read frame data from serial port
    // Parse and return SMPFrame
    return nil, nil
}

func (s *SerialTransport) Close() error {
    return s.port.Close()
}
```

### WebSocket Transport

```go
type WebSocketTransport struct {
    conn *websocket.Conn
}

func (w *WebSocketTransport) Send(frame *simple_smp.SMPFrame) error {
    data, err := simple_smp.SMPFrameToFrame(frame)
    if err != nil {
        return err
    }
    
    return w.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (w *WebSocketTransport) Receive() (*simple_smp.SMPFrame, error) {
    _, data, err := w.conn.ReadMessage()
    if err != nil {
        return nil, err
    }
    
    return simple_smp.FrameToSMPFrame(data)
}

func (w *WebSocketTransport) Close() error {
    return w.conn.Close()
}
```

## Testing

Run the test suite:

```bash
go test -v
```

Run specific tests:

```bash
go test -v -run TestResetDevice
go test -v -run TestUploadFirmware
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Ensure all tests pass
6. Submit a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## References

- [SMP Protocol Specification](https://docs.zephyrproject.org/latest/services/device_mgmt/smp_protocol.html)
- [MCUboot Documentation](https://docs.mcuboot.com/)
- [CBOR Specification](https://tools.ietf.org/html/rfc7049)
