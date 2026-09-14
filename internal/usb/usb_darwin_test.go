//go:build darwin

package usb

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildFakeGetPrintersResponse crafts a minimal IPP Get-Printers response
// with two printer groups: one USB, one network.
func buildFakeGetPrintersResponse() []byte {
	var b bytes.Buffer
	b.Write([]byte{0x01, 0x01}) // version
	b.Write([]byte{0x00, 0x00}) // status successful-ok
	b.WriteByte(0x01)           // operation-attributes group
	writeTestAttr(&b, 0x47, "attributes-charset", "utf-8")
	b.WriteByte(0x04) // printer 1
	writeTestAttr(&b, 0x42, "printer-name", "OCOM_USB")
	writeTestAttr(&b, 0x45, "device-uri", "usb://OCOM/Thermal?serial=ABC123")
	b.WriteByte(0x04) // printer 2
	writeTestAttr(&b, 0x42, "printer-name", "Office_Laser")
	writeTestAttr(&b, 0x45, "device-uri", "ipp://192.168.1.10/ipp/print")
	b.WriteByte(0x03) // end
	return b.Bytes()
}

func writeTestAttr(b *bytes.Buffer, tag byte, name, value string) {
	b.WriteByte(tag)
	_ = binary.Write(b, binary.BigEndian, uint16(len(name)))
	b.WriteString(name)
	_ = binary.Write(b, binary.BigEndian, uint16(len(value)))
	b.WriteString(value)
}

func TestParsePrinterGroups(t *testing.T) {
	resp := buildFakeGetPrintersResponse()
	status, err := ippStatus(resp)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status != 0x0000 {
		t.Fatalf("unexpected status %x", status)
	}
	groups, err := parsePrinterGroups(resp)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 printer groups, got %d", len(groups))
	}
	if groups[0].attrs["printer-name"] != "OCOM_USB" {
		t.Fatalf("unexpected group 0: %+v", groups[0].attrs)
	}
	if groups[1].attrs["device-uri"] != "ipp://192.168.1.10/ipp/print" {
		t.Fatalf("unexpected group 1: %+v", groups[1].attrs)
	}
	// Operation-group attributes must NOT leak into printer groups.
	if _, ok := groups[0].attrs["attributes-charset"]; ok {
		t.Fatalf("op attribute leaked: %+v", groups[0].attrs)
	}
}

func TestParsePrinterGroupsTruncated(t *testing.T) {
	for _, chop := range []int{0, 1, 2, 3, 5, 10, 20, 40} {
		resp := buildFakeGetPrintersResponse()
		if chop < len(resp) {
			resp = resp[:len(resp)-chop]
		}
		if _, err := parsePrinterGroups(resp); err == nil {
			// chop=0 is the full message and must parse.
			if chop != 0 {
				t.Fatalf("expected error for response chopped by %d", chop)
			}
		}
	}
}

func TestParseUSBURI(t *testing.T) {
	make_, model, serial := parseUSBURI("usb://OCOM/Thermal%20Printer?serial=ABC123")
	if make_ != "OCOM" || model != "Thermal Printer" || serial != "ABC123" {
		t.Fatalf("got %q %q %q", make_, model, serial)
	}
	make_, model, serial = parseUSBURI("usb://Vendor/Model")
	if make_ != "Vendor" || model != "Model" || serial != "" {
		t.Fatalf("got %q %q %q", make_, model, serial)
	}
}

func TestEncodeIPPRequestShape(t *testing.T) {
	msg := encodeIPP(ippOpPrintJob, 42, opAttrs("ipp://localhost/printers/Q", "tester"))
	if len(msg) < 4 || msg[0] != 0x01 || msg[1] != 0x01 {
		t.Fatalf("bad version bytes: %x", msg[:4])
	}
	if binary.BigEndian.Uint16(msg[2:4]) != ippOpPrintJob {
		t.Fatalf("bad op id")
	}
	if msg[len(msg)-1] != 0x03 {
		t.Fatalf("missing end tag")
	}
	if !bytes.Contains(msg, []byte("requesting-user-name")) {
		t.Fatalf("missing user attr")
	}
}
