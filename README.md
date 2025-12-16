# Simple SMP

Simple SMP is an opionated Go library for interacting with the [SMP (Simple Management Protocol)](https://docs.zephyrproject.org/latest/services/device_mgmt/smp_protocol.html) used for device management in embedded systems.

It is created to be used in [`zigbee_home`](https://github.com/ffenix113/zigbee_home), but can be extended freely to add new functionality.

**Note: API is not stable and may change in later revisions.**

## Supported Groups and Commands

- **OS**: Reset (with force)
- **Image**: Upload image


## Installation

```bash
go get github.com/ffenix113/smp
```

## Usage

### Basic SMP Client

```go
import "github.com/ffenix113/smp"

// Create transport and specify device to connect to
transport, err := NewBLETransport(BLETransportConfig{
    // In this case device name will be used, but it can also be an address(uuid/mac/... depending on the platform)
    Name: deviceName,
})

// Create a new SMP client with your transport
client := smp.NewSMPClient(yourTransport)

// Upload image with windowed parallelism.
// Make sure that context's timeout is sufficient for this operation!
err := client.UploadImageWithWindows(ctx, maxWindows, data, chunkSize, callback)
```

### Lower level

If functionality defined in this library is not sufficient - it is possible to send SMP frames directly:

```go
// Build reset request struct
data, err := EncodeCBOR(ResetRequest{Force: false})
if err != nil {
    return fmt.Errorf("failed to encode reset request: %v", err)
}

// Create SMP frame for reset command
// Sequence number is already set, but can be changed as necessary
frame := CreateFrame(SMPOpWriteRequest, SMPGroupOS, SMPCmdReset, data)

// Send the frame
response, err := c.transport.Send(ctx, frame)
if err != nil {
    return fmt.Errorf("failed to send reset frame: %v", err)
}

// Parse response, assuming that response was success
// `ResetResponse` type contains `Err` struct as well.
resetResp, err := DecodeCBOR[ResetResponse](response.Data)
if err != nil {
    return fmt.Errorf("failed to parse reset response: %v", err)
}
```

## Transport Interface

The library requires a transport implementation to communicate with the device. The transport must implement the `Transport` interface:

```go
type Transport interface {
    Close() error
    Connect(ctx context.Context) error
    Send(ctx context.Context, frame SMPFrame) (SMPFrame, error)
}
```

Tranport constructor must accept argument that will define which device should be connected to, and other optional parameters.

Transports iplemetations currently must be synchronous, even if underlying transport is asynchronous.
Asynchronous operations then can be defined with use of contexts and goroutines.

### Implemented Transports

- Bluetooth Low Energy (BLE)
