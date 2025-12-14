package simple_smp

import (
	"context"
	"fmt"
)

// ResetDevice sends a device reset command with optional force parameter
func (c *SMPClient) ResetDevice(ctx context.Context, force bool) error {
	// Build reset request struct
	req := BuildResetRequest(force)
	// Encode to CBOR manually
	data, err := EncodeCBOR(req)
	if err != nil {
		return fmt.Errorf("failed to encode reset request: %v", err)
	}

	// Create SMP frame for reset command
	frame := c.CreateFrame(SMPOpWriteRequest, SMPGroupOS, SMPCmdReset, data)

	// Send the frame
	response, err := c.transport.Send(ctx, frame)
	if err != nil {
		return fmt.Errorf("failed to send reset frame: %v", err)
	}

	// Validate response
	if err := response.ValidateFrame(); err != nil {
		return fmt.Errorf("invalid reset response frame: %v", err)
	}

	// Parse response
	resetResp, err := ParseResetResponse(response.Data)
	if err != nil {
		return fmt.Errorf("failed to parse reset response: %v", err)
	}

	// Check for errors in response
	if resetResp.Err != nil {
		return fmt.Errorf("reset command failed: group=%d, rc=%d", resetResp.Err.Group, resetResp.Err.Rc)
	}

	return nil
}
