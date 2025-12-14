package simple_smp

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestBLETransportConnectAndReset(t *testing.T) {
	// This test will fail normally since there's no BLE device available
	// When a compatible BLE device is present, it should pass

	// Skip test if no BLE adapter is available - this ensures the test
	// can be run without requiring physical hardware to fail gracefully
	// t.Skip("BLE transport test - requires physical BLE device for passing test")

	// Create a new BLE transport
	transport, err := NewBLETransport()
	if err != nil {
		t.Fatalf("create ble transport: %s", err.Error())
	}

	// Define device parameters - in a real test, this would use an actual device name
	params := url.Values{}
	params.Set("name", "ZBHome nrf52dk")

	// Create context with timeout to avoid hanging
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to connect to the device - this should fail gracefully without mocks
	err = transport.Connect(ctx, params)
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
	// Create a new BLE transport
	transport, err := NewBLETransport()
	if err != nil {
		t.Fatalf("create ble transport: %s", err.Error())
	}

	// Define device parameters - in a real test, this would use an actual device name
	params := url.Values{}
	params.Set("name", "ZBHome nrf54l")

	// Create context with timeout to avoid hanging
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Try to connect to the device - this should fail gracefully without mocks
	err = transport.Connect(ctx, params)
	if err != nil {
		// Test should fail gracefully when device is not found
		t.Fatalf("Expected failure when connecting to device: %s", err.Error())
	}

	// If we get here, the device was found and connected
	// Send Reset command as SMP frame - this would only work if device is actually connected
	client := NewSMPClient(transport)

	imgData, _ := os.ReadFile("/Users/ffenix/Projects/zigbee_home/_ota3/build/_ota3/zephyr/zephyr.signed.bin")

	var prevUploaded uint32
	var totalUploaded uint32
	go func() {
		for totalUploaded < uint32(len(imgData)) {
			totalUploaded := totalUploaded

			sizeDiff := totalUploaded - prevUploaded
			prevUploaded = totalUploaded

			speed := float64(sizeDiff) / 1024 / 2

			slog.Info("uploaded chunk", "totalBytes", totalUploaded, "speed", speed)

			time.Sleep(2 * time.Second)
		}
	}()

	const chunkSize = 320
	err = client.UploadFirmware2(ctx, imgData, chunkSize, func(req FirmwareUploadRequest) {
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
