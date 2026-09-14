package usb

import (
	"fmt"
	"sync"
)

// MockUSB is an in-memory USBPrinter for tests and for environments without
// USB hardware. It records every Write so tests can assert exact bytes.
type MockUSB struct {
	mu      sync.Mutex
	Devices []USBDevice
	Opened  map[string]bool
	Writes  map[string][][]byte

	ListErr  error
	OpenErr  error
	CloseErr error
	WriteErr error
}

// NewMock returns a MockUSB with two scripted devices.
func NewMock() *MockUSB {
	return &MockUSB{
		Devices: []USBDevice{
			{
				ID: "usb:04B8:0202:ABC123", Name: "Mock Thermal Printer",
				VendorID: 0x04B8, ProductID: 0x0202, Serial: "ABC123",
				Manufacturer: "MockVendor", Product: "Mock Thermal",
				Detail: "mock backend",
			},
			{
				ID: "usb:0416:5011:XYZ789", Name: "Mock Label Printer",
				VendorID: 0x0416, ProductID: 0x5011, Serial: "XYZ789",
				Manufacturer: "MockVendor", Product: "Mock Label",
				Detail: "mock backend",
			},
		},
		Opened: make(map[string]bool),
		Writes: make(map[string][][]byte),
	}
}

// ListDevices implements USBPrinter.
func (m *MockUSB) ListDevices() ([]USBDevice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ListErr != nil {
		return nil, m.ListErr
	}
	out := make([]USBDevice, len(m.Devices))
	copy(out, m.Devices)
	return out, nil
}

// Open implements USBPrinter.
func (m *MockUSB) Open(deviceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.OpenErr != nil {
		return m.OpenErr
	}
	if !m.exists(deviceID) {
		return fmt.Errorf("%w: %s", ErrDeviceNotFound, deviceID)
	}
	m.Opened[deviceID] = true
	return nil
}

// Close implements USBPrinter.
func (m *MockUSB) Close(deviceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CloseErr != nil {
		return m.CloseErr
	}
	delete(m.Opened, deviceID)
	return nil
}

// Write implements USBPrinter. It records a copy of the payload.
func (m *MockUSB) Write(deviceID string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.WriteErr != nil {
		return m.WriteErr
	}
	if !m.exists(deviceID) {
		return fmt.Errorf("%w: %s", ErrDeviceNotFound, deviceID)
	}
	if len(data) == 0 {
		return fmt.Errorf("empty payload")
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	m.Writes[deviceID] = append(m.Writes[deviceID], cp)
	return nil
}

func (m *MockUSB) exists(deviceID string) bool {
	for _, d := range m.Devices {
		if d.ID == deviceID {
			return true
		}
	}
	return false
}

// Written returns all payloads recorded for deviceID.
func (m *MockUSB) Written(deviceID string) [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]byte, len(m.Writes[deviceID]))
	copy(out, m.Writes[deviceID])
	return out
}

// IsOpen reports whether deviceID is currently open.
func (m *MockUSB) IsOpen(deviceID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Opened[deviceID]
}

// Reset clears recorded state and injected errors.
func (m *MockUSB) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Opened = make(map[string]bool)
	m.Writes = make(map[string][][]byte)
	m.ListErr, m.OpenErr, m.CloseErr, m.WriteErr = nil, nil, nil, nil
}
