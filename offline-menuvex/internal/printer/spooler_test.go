package printer

import (
	"context"
	"strings"
	"testing"
)

func TestParseQueueListNormalizesAndDeduplicates(t *testing.T) {
	raw := "POS-80\n\n EPSON-TM-T20 \nPOS-80\nbad\x01name\n"
	got := ParseQueueList(raw)
	if len(got) != 2 || got[0] != "EPSON-TM-T20" || got[1] != "POS-80" {
		t.Fatalf("queues = %#v", got)
	}
}

func TestSendSpoolerRawValidatesBeforeInvokingLp(t *testing.T) {
	err := sendSpoolerRaw(context.Background(), "bad\nqueue", []byte("x"))
	op, ok := err.(*OpError)
	if !ok || op.Code != "INVALID_CONFIG" {
		t.Fatalf("expected INVALID_CONFIG, got %#v", err)
	}
	if !strings.Contains(err.Error(), "صف") {
		t.Fatalf("error = %q", err)
	}
}

func TestListQueuesIsBoundedAndNeverPanics(t *testing.T) {
	queues, err := ListQueues(context.Background())
	if err != nil {
		if strings.TrimSpace(err.Error()) == "" {
			t.Fatal("empty error message")
		}
		return
	}
	for _, q := range queues {
		if strings.TrimSpace(q) == "" {
			t.Fatalf("empty queue name in %#v", queues)
		}
	}
}

func TestDiscoverUSBIsReadOnlyAndDeduplicated(t *testing.T) {
	devices, err := DiscoverUSB()
	if err != nil {
		t.Fatalf("discovery must not fail on systems without USB: %v", err)
	}
	seen := map[string]bool{}
	for _, d := range devices {
		key := d.Identity()
		if seen[key] {
			t.Fatalf("duplicate device %+v", d)
		}
		seen[key] = true
	}
}
