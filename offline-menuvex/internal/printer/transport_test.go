package printer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

type testAddr struct{}

func (testAddr) Network() string { return "test" }
func (testAddr) String() string  { return "test" }

type scriptedConn struct {
	mu      sync.Mutex
	writeN  int
	writeEr error
	written bytes.Buffer
}

func (c *scriptedConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *scriptedConn) Close() error                     { return nil }
func (c *scriptedConn) LocalAddr() net.Addr              { return testAddr{} }
func (c *scriptedConn) RemoteAddr() net.Addr             { return testAddr{} }
func (c *scriptedConn) SetDeadline(time.Time) error      { return nil }
func (c *scriptedConn) SetReadDeadline(time.Time) error  { return nil }
func (c *scriptedConn) SetWriteDeadline(time.Time) error { return nil }
func (c *scriptedConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.written.Write(p)
	n := c.writeN
	if n > len(p) {
		n = len(p)
	}
	return n, c.writeEr
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

type scriptedWriteCloser struct {
	n  int
	er error
}

func (w scriptedWriteCloser) Write(p []byte) (int, error) { return w.n, w.er }
func (w scriptedWriteCloser) Close() error                { return nil }

func TestSenderNetworkDeliversAllBytes(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		n, _ := conn.Read(buf)
		got <- append([]byte(nil), buf[:n]...)
	}()
	addr := ln.Addr().(*net.TCPAddr)
	s := NewSender()
	payload := []byte{0x1B, 0x40, 'h', 'i'}
	if err := s.Send(context.Background(), Profile{
		Kind: KindNetwork, Host: "127.0.0.1", Port: addr.Port, PaperMM: 80,
	}, payload); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-got:
		if !bytes.Equal(data, payload) {
			t.Fatalf("received %x, want %x", data, payload)
		}
	case <-time.After(time.Second):
		t.Fatal("printer received nothing")
	}
}

func TestSenderClassifiesFailures(t *testing.T) {
	// Connect failure is safe to retry.
	s := &Sender{
		Dial:           func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("refused") },
		ConnectTimeout: time.Second,
		WriteTimeout:   time.Second,
	}
	err := s.Send(context.Background(), Profile{Kind: KindNetwork, Host: "127.0.0.1", Port: 9100}, []byte("x"))
	if !IsRetryable(err) {
		t.Fatalf("connect failure must be retryable, got %#v", err)
	}
	// A partial write can never be retried automatically.
	partial := &scriptedConn{writeN: 2, writeEr: errors.New("broken pipe")}
	s.Dial = func(context.Context, string, string) (net.Conn, error) { return partial, nil }
	err = s.Send(context.Background(), Profile{Kind: KindNetwork, Host: "127.0.0.1", Port: 9100}, []byte("abcdef"))
	if !IsUncertain(err) {
		t.Fatalf("partial write must be uncertain, got %#v", err)
	}
	// A write timeout is ambiguous even with zero bytes.
	timeout := &scriptedConn{writeN: 0, writeEr: timeoutErr{}}
	s.Dial = func(context.Context, string, string) (net.Conn, error) { return timeout, nil }
	err = s.Send(context.Background(), Profile{Kind: KindNetwork, Host: "127.0.0.1", Port: 9100}, []byte("abcdef"))
	if !IsUncertain(err) {
		t.Fatalf("timeout must be uncertain, got %#v", err)
	}
}

func TestSenderRejectsUnsafePayloads(t *testing.T) {
	s := NewSender()
	if err := s.Send(context.Background(), Profile{Kind: KindNetwork, Host: "127.0.0.1", Port: 9100}, nil); err == nil {
		t.Fatal("empty payload must be rejected")
	}
	big := make([]byte, PayloadLimit+1)
	if err := s.Send(context.Background(), Profile{Kind: KindNetwork, Host: "127.0.0.1", Port: 9100}, big); err == nil {
		t.Fatal("oversized payload must be rejected")
	}
}

func TestSenderSpooler(t *testing.T) {
	var gotQueue string
	var gotPayload []byte
	s := NewSender()
	s.QueueSend = func(ctx context.Context, queue string, payload []byte) error {
		gotQueue, gotPayload = queue, append([]byte(nil), payload...)
		return nil
	}
	p := Profile{Kind: KindSpooler, QueueName: "POS-80"}
	if err := s.Send(context.Background(), p, []byte("raw")); err != nil {
		t.Fatal(err)
	}
	if gotQueue != "POS-80" || string(gotPayload) != "raw" {
		t.Fatalf("queue=%q payload=%q", gotQueue, gotPayload)
	}
	s.QueueSend = func(context.Context, string, []byte) error { return errors.New("lp failed") }
	err := s.Send(context.Background(), p, []byte("raw"))
	if !IsUncertain(err) {
		t.Fatalf("spooler failure must be uncertain, got %#v", err)
	}
}

func TestSenderUSB(t *testing.T) {
	s := NewSender()
	s.USBAvailable = func() error { return nil }
	profile := Profile{Kind: KindUSB, USB: USBDevice{DevicePath: "/dev/usb/lp0"}}
	s.USBOpen = func(string) (io.WriteCloser, error) { return scriptedWriteCloser{n: 3}, nil }
	if err := s.Send(context.Background(), profile, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	s.USBOpen = func(string) (io.WriteCloser, error) {
		return scriptedWriteCloser{n: 0, er: errors.New("denied")}, nil
	}
	if err := s.Send(context.Background(), profile, []byte("abc")); !IsRetryable(err) {
		t.Fatalf("zero-byte USB failure must be retryable, got %#v", err)
	}
	s.USBOpen = func(string) (io.WriteCloser, error) {
		return scriptedWriteCloser{n: 1, er: errors.New("unplugged")}, nil
	}
	if err := s.Send(context.Background(), profile, []byte("abc")); !IsUncertain(err) {
		t.Fatalf("partial USB write must be uncertain, got %#v", err)
	}
	s.USBAvailable = func() error { return ErrUSBSupportedForTest }
	if err := s.Send(context.Background(), profile, []byte("abc")); err == nil {
		t.Fatal("unsupported USB must fail with guidance")
	}
}

var ErrUSBSupportedForTest = errors.New("USB unavailable in test")
