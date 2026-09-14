package api

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/menuvex/novex-printer-agent/internal/printer"
	"github.com/menuvex/novex-printer-agent/internal/security"
)

// Minimal RFC 6455 WebSocket server (stdlib only) for real-time printer
// events: printer.connected / printer.disconnected / printer.error.
//
// WebSocket is strictly optional: every print operation works over plain
// HTTP. Browsers authenticate with ?token=... since they cannot set
// request headers on a WebSocket handshake.

const (
	wsGUID            = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	wsMaxMessage      = 1 << 20 // 1 MiB inbound cap
	wsWriteTimeout    = 5 * time.Second
	wsPingInterval    = 30 * time.Second
	wsCloseTimeout    = 5 * time.Second
	wsMaxHeaderTokens = 8
)

// Hub fans events out to connected WebSocket clients.
type Hub struct {
	mu      sync.Mutex
	clients map[*wsClient]bool
	closed  bool
}

// NewHub creates an empty Hub.
func NewHub() *Hub {
	return &Hub{clients: make(map[*wsClient]bool)}
}

// Broadcast marshals v once and sends it as a text frame to every client.
// Slow or dead clients are dropped; broadcast never blocks on them.
func (h *Hub) Broadcast(v interface{}) {
	payload, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.Lock()
	clients := make([]*wsClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		if err := c.sendText(payload); err != nil {
			h.remove(c)
			c.close()
		}
	}
}

func (h *Hub) add(c *wsClient) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	h.clients[c] = true
	return true
}

func (h *Hub) remove(c *wsClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// Count returns the number of connected clients.
func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Close drops all clients.
func (h *Hub) Close() {
	h.mu.Lock()
	clients := make([]*wsClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.clients = make(map[*wsClient]bool)
	h.closed = true
	h.mu.Unlock()
	for _, c := range clients {
		c.close()
	}
}

// wsClient is one WebSocket connection.
type wsClient struct {
	conn   net.Conn
	reader *bufio.Reader
	sendMu sync.Mutex
	done   chan struct{}
	once   sync.Once
}

func (c *wsClient) close() {
	c.once.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

// sendFrame writes one server-to-client frame (never masked).
func (c *wsClient) sendFrame(opcode byte, payload []byte) error {
	var header []byte
	header = append(header, 0x80|opcode) // FIN + opcode
	n := len(payload)
	switch {
	case n <= 125:
		header = append(header, byte(n))
	case n <= 65535:
		header = append(header, 126, byte(n>>8), byte(n))
	default:
		header = append(header, 127,
			byte(n>>56), byte(n>>48), byte(n>>40), byte(n>>32),
			byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	for len(payload) > 0 {
		m, err := c.conn.Write(payload)
		if err != nil {
			return err
		}
		payload = payload[m:]
	}
	return nil
}

func (c *wsClient) sendText(payload []byte) error {
	return c.sendFrame(0x1, payload)
}

func (c *wsClient) sendPong(payload []byte) error {
	return c.sendFrame(0xA, payload)
}

func (c *wsClient) sendPing() error {
	return c.sendFrame(0x9, nil)
}

func (c *wsClient) sendClose(code int) {
	payload := []byte{byte(code >> 8), byte(code)}
	_ = c.sendFrame(0x8, payload)
}

// readFrame reads one client-to-server frame. Clients MUST mask; unmasked
// frames are a protocol error. Fragmentation is not supported (browsers
// send control/event-size messages in a single frame).
func (c *wsClient) readFrame() (opcode byte, payload []byte, err error) {
	b1, err := c.reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	b2, err := c.reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	fin := b1&0x80 != 0
	opcode = b1 & 0x0F
	masked := b2&0x80 != 0
	length := int64(b2 & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if err := readFull(c.reader, ext[:]); err != nil {
			return 0, nil, err
		}
		length = int64(ext[0])<<8 | int64(ext[1])
	case 127:
		var ext [8]byte
		if err := readFull(c.reader, ext[:]); err != nil {
			return 0, nil, err
		}
		length = int64(ext[0])<<56 | int64(ext[1])<<48 | int64(ext[2])<<40 |
			int64(ext[3])<<32 | int64(ext[4])<<24 | int64(ext[5])<<16 |
			int64(ext[6])<<8 | int64(ext[7])
	}
	if length < 0 || length > wsMaxMessage {
		return 0, nil, fmt.Errorf("message too large")
	}
	var mask [4]byte
	if masked {
		if err := readFull(c.reader, mask[:]); err != nil {
			return 0, nil, err
		}
	} else {
		return 0, nil, fmt.Errorf("unmasked client frame")
	}
	payload = make([]byte, length)
	if err := readFull(c.reader, payload); err != nil {
		return 0, nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	if !fin {
		return 0, nil, fmt.Errorf("fragmented frames not supported")
	}
	return opcode, payload, nil
}

func readFull(r *bufio.Reader, b []byte) error {
	for len(b) > 0 {
		n, err := r.Read(b)
		b = b[n:]
		if err != nil {
			return err
		}
	}
	return nil
}

// serve runs the read loop until the client goes away.
func (c *wsClient) serve(h *Hub) {
	defer func() {
		h.remove(c)
		c.close()
	}()
	// Heartbeat: browsers answer pings automatically.
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-c.done:
				return
			case <-ticker.C:
				if err := c.sendPing(); err != nil {
					c.close()
					return
				}
			}
		}
	}()
	for {
		op, payload, err := c.readFrame()
		if err != nil {
			return
		}
		switch op {
		case 0x8: // close
			c.sendClose(1000)
			return
		case 0x9: // ping
			if err := c.sendPong(payload); err != nil {
				return
			}
		case 0xA: // pong
		case 0x1, 0x2: // text/binary: no client commands in v1; ignore
		default:
			c.sendClose(1003)
			return
		}
	}
}

// handleWS upgrades GET /ws to WebSocket after token + origin checks.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !headerContainsToken(r.Header.Get("Upgrade"), "websocket") ||
		!headerContainsToken(r.Header.Get("Connection"), "upgrade") {
		writeAPIError(w, wsHandshakeError("not a websocket upgrade request"))
		return
	}
	if strings.TrimSpace(r.Header.Get("Sec-WebSocket-Version")) != "13" {
		writeAPIError(w, wsHandshakeError("unsupported Sec-WebSocket-Version (want 13)"))
		return
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		writeAPIError(w, wsHandshakeError("missing Sec-WebSocket-Key"))
		return
	}
	if !security.CheckQueryToken(s.cfg.Token, r.URL.Query().Get("token")) {
		writeAPIError(w, printer.NewError(printer.CodeUnauthorized, "", "missing or invalid token (use /ws?token=...)"))
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		writeAPIError(w, wsHandshakeError("websocket hijacking not supported"))
		return
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		writeAPIError(w, wsHandshakeError("websocket hijack failed"))
		return
	}
	accept := wsAcceptKey(key)
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	_ = conn.SetWriteDeadline(time.Now().Add(wsCloseTimeout))
	if _, err := rw.WriteString(resp); err != nil {
		_ = conn.Close()
		return
	}
	if err := rw.Flush(); err != nil {
		_ = conn.Close()
		return
	}
	client := &wsClient{conn: conn, reader: rw.Reader, done: make(chan struct{})}
	if !s.hub.add(client) {
		_ = conn.Close()
		return
	}
	// Send a hello so clients can confirm the subscription.
	_ = client.sendText([]byte(`{"event":"agent.hello","version":` + strconv.Quote(s.version) + `}`))
	go client.serve(s.hub)
}

// wsAcceptKey derives Sec-WebSocket-Accept from the client key.
func wsAcceptKey(key string) string {
	sum := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// headerContainsToken reports whether a comma-separated header contains tok.
func headerContainsToken(header, tok string) bool {
	parts := strings.Split(header, ",")
	if len(parts) > wsMaxHeaderTokens {
		parts = parts[:wsMaxHeaderTokens]
	}
	for _, p := range parts {
		if strings.EqualFold(strings.TrimSpace(p), tok) {
			return true
		}
	}
	return false
}

// wsHandshakeError builds a handshake-failure API error.
func wsHandshakeError(msg string) *printer.Error {
	return printer.NewError(printer.CodeInvalidRequest, "", "%s", msg)
}
