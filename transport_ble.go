package smp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

var characteristicSMPUUID, _ = bluetooth.ParseUUID("da2e7828-fbce-4e01-ae9e-261174997c48")

var _ Transport = (*BLETransport)(nil)

type BLETransport struct {
	cfg BLETransportConfig

	adapter *bluetooth.Adapter
	device  bluetooth.Device

	smpCharacteristic bluetooth.DeviceCharacteristic

	rcv chan SMPFrame

	cbs   map[uint8]func(frame SMPFrame)
	cbsMu sync.Mutex
}

type BLETransportConfig struct {
	Name    string
	Address string
}

func NewBLETransport(cfg BLETransportConfig) (*BLETransport, error) {
	if err := bluetooth.DefaultAdapter.Enable(); err != nil {
		return nil, fmt.Errorf("enable bluetooth adapter: %w", err)
	}

	return &BLETransport{
		adapter: bluetooth.DefaultAdapter,
		cfg:     cfg,
		rcv:     make(chan SMPFrame, 16),
		cbs:     make(map[uint8]func(frame SMPFrame)),
	}, nil
}

func (b *BLETransport) Connect(ctx context.Context) error {
	var found bool
	var deviceAddr bluetooth.Address

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	err := b.adapter.Scan(func(a *bluetooth.Adapter, sr bluetooth.ScanResult) {
		slog.Debug("found ble device", "name", sr.LocalName(), "addr", sr.Address)

		nameMatch := b.cfg.Name != "" && sr.LocalName() == b.cfg.Name
		addrMatch := b.cfg.Address != "" && sr.Address.String() == b.cfg.Address

		if !nameMatch && !addrMatch {
			return
		}

		deviceAddr = sr.Address
		found = true

		cancel()
		_ = b.adapter.StopScan()
	})
	if err != nil {
		return fmt.Errorf("start ble scan: %w", err)
	}

	slog.Info("started ble scan", "params", b.cfg)

	<-ctx.Done()
	_ = b.adapter.StopScan()

	if !found {
		return errors.New("device could not be found")
	}

	dev, err := b.adapter.Connect(deviceAddr, bluetooth.ConnectionParams{
		ConnectionTimeout: bluetooth.NewDuration(10 * time.Second),
		// MinInterval:       bluetooth.NewDuration(10 * time.Second),
		// MaxInterval:       bluetooth.NewDuration(50 * time.Second),
		Timeout: bluetooth.NewDuration(10 * time.Second),
	})
	if err != nil {
		return fmt.Errorf("connect ble: %w", err)
	}

	b.device = dev

	if err := b.setSMPCharacteristic(); err != nil {
		return fmt.Errorf("discover smp: %w", err)
	}

	if err := b.receiveCallback(); err != nil {
		return fmt.Errorf("set receive callback: %w", err)
	}

	return nil
}

// Close implements Transport.
func (b *BLETransport) Close() error {
	if err := b.device.Disconnect(); err != nil {
		return fmt.Errorf("disconnect ble: %w", err)
	}

	return nil
}

// Send implements Transport.
func (b *BLETransport) Send(ctx context.Context, frame SMPFrame) (SMPFrame, error) {
	// slog.Info("send smp packet", "packet", frame, "data", hex.Dump(data))

	data, err := SMPFrameToFrame(frame)
	if err != nil {
		return SMPFrame{}, fmt.Errorf("convert frame to bytes: %w", err)
	}

	_, err = b.smpCharacteristic.WriteWithoutResponse(data)
	if err != nil {
		return SMPFrame{}, fmt.Errorf("write data: %w", err)
	}

	return b.waitForResp(ctx, frame.Header.SequenceNum)
}

func (b *BLETransport) setSMPCharacteristic() error {
	services, err := b.device.DiscoverServices([]bluetooth.UUID{bluetooth.ServiceUUIDSMP})
	if err != nil {
		return fmt.Errorf("get services: %w", err)
	}

	if len(services) != 1 {
		return errors.New("got no matching services")
	}

	smpService := services[0]

	chars, err := smpService.DiscoverCharacteristics([]bluetooth.UUID{characteristicSMPUUID})
	if err != nil {
		return fmt.Errorf("get characteristics: %w", err)
	}

	if len(chars) == 0 {
		return errors.New("characteristic not found")
	}

	b.smpCharacteristic = chars[0]

	return nil
}

func (b *BLETransport) receiveCallback() error {
	err := b.smpCharacteristic.EnableNotifications(func(buf []byte) {
		smp, err := FrameToSMPFrame(buf)
		if err != nil {
			slog.Error("decode received data", "err", err.Error())

			return
		}

		b.cbsMu.Lock()
		defer b.cbsMu.Unlock()

		seq := smp.Header.SequenceNum
		if cb := b.cbs[seq]; cb != nil {
			delete(b.cbs, seq)

			cb(smp)
		}
	})
	if err != nil {
		return fmt.Errorf("enable characteristic notifications: %w", err)
	}

	return nil
}

func (b *BLETransport) waitForResp(ctx context.Context, seq uint8) (SMPFrame, error) {
	if _, ok := ctx.Deadline(); !ok {
		return SMPFrame{}, errors.New("context must have deadline set for wait")
	}

	resp := make(chan SMPFrame)

	b.cbsMu.Lock()
	b.cbs[seq] = func(frame SMPFrame) {
		resp <- frame
	}
	b.cbsMu.Unlock()

	defer func() {
		b.cbsMu.Lock()
		defer b.cbsMu.Unlock()

		delete(b.cbs, seq)
	}()

	select {
	case <-ctx.Done():
		err := ctx.Err()
		if errors.Is(err, context.DeadlineExceeded) {
			return SMPFrame{}, ErrWaitTimeout
		}

		return SMPFrame{}, ctx.Err()
	case frame := <-resp:
		return frame, nil
	}
}
