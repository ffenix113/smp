package smp

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestBLETransportConnectAndReset(t *testing.T) {
	// This test will fail normally since there's no BLE device available
	// When a compatible BLE device is present, it should pass

	// Skip test if no BLE adapter is available - this ensures the test
	// can be run without requiring physical hardware to fail gracefully
	t.Skip("BLE transport test - requires physical BLE device for passing test")

	// Create a new BLE transport
	transport, err := NewBLETransport(BLETransportConfig{
		Name: "ZBHome nrf52dk",
	})
	if err != nil {
		t.Fatalf("create ble transport: %s", err.Error())
	}

	// Create context with timeout to avoid hanging
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to connect to the device - this should fail gracefully without mocks
	err = transport.Connect(ctx)
	if err != nil {
		// Test should fail gracefully when device is not found
		t.Fatalf("Expected failure when connecting to device: %s", err.Error())
	}

	// If we get here, the device was found and connected
	// Send Reset command as SMP frame - this would only work if device is actually connected
	client := NewSMPClient(transport)
	err = client.ResetDevice(ctx, true)
	if err != nil {
		t.Fatalf("Failed to send Reset command: %s", err.Error())
	}

	// res, err := client.transport.Receive()
	// t.Log(res, err)

	// Close the connection
	err = transport.Close()
	if err != nil {
		t.Fatalf("Failed to close connection: %s", err.Error())
	}
}

func TestBLETransportUploadImg(t *testing.T) {
	t.Skip("BLE transport test - requires physical BLE device for passing test")

	const deviceName = "ZBHome nrf54l"
	const imgPath = "~/firmware/build/firmware/zephyr/zephyr.signed.bin"

	// Create a new BLE transport
	transport, err := NewBLETransport(BLETransportConfig{
		Name: deviceName,
	})
	if err != nil {
		t.Fatalf("create ble transport: %s", err.Error())
	}

	// Create context with timeout to avoid hanging
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Try to connect to the device - this should fail gracefully without mocks
	err = transport.Connect(ctx)
	if err != nil {
		// Test should fail gracefully when device is not found
		t.Fatalf("Expected failure when connecting to device: %s", err.Error())
	}

	// If we get here, the device was found and connected
	// Send Reset command as SMP frame - this would only work if device is actually connected
	client := NewSMPClient(transport)

	imgData, _ := os.ReadFile(imgPath)

	var prevUploaded uint32
	var totalUploaded uint32
	go func() {
		for totalUploaded < uint32(len(imgData)) {
			totalUploaded := totalUploaded

			sizeDiff := totalUploaded - prevUploaded
			prevUploaded = totalUploaded

			speed := float64(sizeDiff) / 1024 / 2

			t.Logf("uploaded chunk, totalBytes: %d, speed: %.02f", totalUploaded, speed)

			time.Sleep(2 * time.Second)
		}
	}()

	const chunkSize = 320
	err = client.UploadImageWithWindows(ctx, 5, imgData, chunkSize, func(req FirmwareUploadRequest) {
		totalUploaded += uint32(len(req.Data))
	})
	if err != nil {
		t.Fatalf("upload image: %s", err.Error())
	}

	// Close the connection
	err = transport.Close()
	if err != nil {
		t.Fatalf("Failed to close connection: %s", err.Error())
	}
}
