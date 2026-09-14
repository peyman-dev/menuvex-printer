// Package api exposes the localhost HTTP + WebSocket API.
//
// Endpoints (all JSON; all but /health require "Authorization: Bearer <token>"):
//
//	GET    /health
//	GET    /api/v1/info
//	GET    /api/v1/printers[?scan=true&ports=9100&scanTimeoutMs=8000]
//	POST   /api/v1/printers
//	GET    /api/v1/printers/:id
//	DELETE /api/v1/printers/:id
//	POST   /api/v1/printers/:id/connect
//	POST   /api/v1/printers/:id/disconnect
//	POST   /api/v1/printers/:id/print
//	GET    /api/v1/printers/:id/status
//	GET    /ws?token=...
//
// Printer IDs in the path must be URL-escaped (encodeURIComponent).
package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/menuvex/novex-printer-agent/internal/config"
	"github.com/menuvex/novex-printer-agent/internal/network"
	"github.com/menuvex/novex-printer-agent/internal/printer"
	"github.com/menuvex/novex-printer-agent/internal/security"
	"github.com/menuvex/novex-printer-agent/internal/usb"
)

// MaxPrintBytes caps a single print payload (10 MiB — far above any receipt).
const MaxPrintBytes = 10 << 20

// MaxScanTimeout caps a single LAN scan request.
const MaxScanTimeout = 30 * time.Second

// Server is the localhost API server.
type Server struct {
	reg       *printer.Registry
	cfg       *config.Config
	version   string
	hub       *Hub
	startTime time.Time
	httpSrv   *http.Server
}

// NewServer builds the API server.
func NewServer(reg *printer.Registry, cfg *config.Config, version string) *Server {
	s := &Server{
		reg:       reg,
		cfg:       cfg,
		version:   version,
		hub:       NewHub(),
		startTime: time.Now(),
	}
	s.reg.SetEventHandler(func(ev printer.Event) {
		s.hub.Broadcast(ev)
	})
	s.httpSrv = &http.Server{
		Addr:              cfg.Addr(),
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return s
}

// Start serves HTTP until Shutdown or a fatal error.
func (s *Server) Start() error {
	return s.httpSrv.ListenAndServe()
}

// Shutdown stops the server gracefully.
func (s *Server) Shutdown(ctx context.Context) error {
	s.hub.Close()
	return s.httpSrv.Shutdown(ctx)
}

// Hub exposes the WebSocket hub (used by tests).
func (s *Server) Hub() *Hub {
	return s.hub
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/api/v1/info", s.requireAuth(s.handleInfo))
	mux.HandleFunc("/api/v1/printers", s.requireAuth(s.handlePrinters))
	mux.HandleFunc("/api/v1/printers/", s.requireAuth(s.handlePrinterItem))
	mux.HandleFunc("/", s.requireAuth(s.handleNotFound))
	return security.CORS(s.cfg.TrustedOrigins, http.HandlerFunc(s.handleForbiddenOrigin), mux)
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !security.CheckToken(s.cfg.Token, r.Header.Get("Authorization")) {
			writeAPIError(w, printer.NewError(printer.CodeUnauthorized, "", "missing or invalid bearer token"))
			return
		}
		next(w, r)
	}
}

// ---------- generic handlers ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":        "ok",
		"version":       s.version,
		"platform":      runtime.GOOS,
		"arch":          runtime.GOARCH,
		"time":          time.Now().UTC().Format(time.RFC3339),
		"uptimeSeconds": int(time.Since(s.startTime).Seconds()),
	})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	usbSupported, usbDetail := usb.SupportInfo()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"version":      s.version,
		"platform":     runtime.GOOS,
		"arch":         runtime.GOARCH,
		"host":         s.cfg.Host,
		"port":         s.cfg.Port,
		"usbSupported": usbSupported,
		"usbDetail":    usbDetail,
		"time":         time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeAPIError(w, printer.NewError(printer.CodeNotFound, "", "unknown endpoint %s %s", r.Method, r.URL.Path))
}

func (s *Server) handleForbiddenOrigin(w http.ResponseWriter, r *http.Request) {
	writeAPIError(w, printer.NewError(printer.CodeForbiddenOrigin, "", "origin %q is not trusted", r.Header.Get("Origin")))
}

// ---------- printers collection ----------

func (s *Server) handlePrinters(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListPrinters(w, r)
	case http.MethodPost:
		s.handleRegisterPrinter(w, r)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleListPrinters(w http.ResponseWriter, r *http.Request) {
	printers, err := s.reg.ListPrinters()
	if err != nil {
		writeAPIError(w, err)
		return
	}
	scanned := false
	if parseBoolQuery(r.URL.Query().Get("scan")) {
		scanned = true
		for _, hit := range s.runScan(r) {
			id := printer.TCPPrinterID(hit.Address, hit.Port)
			if containsPrinter(printers, id) {
				continue
			}
			printers = append(printers, printer.Printer{
				ID:      id,
				Name:    hit.Address,
				Type:    printer.TypeNetwork,
				Status:  printer.StatusAvailable,
				Address: hit.Address,
				Port:    hit.Port,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"printers": printers,
		"scanned":  scanned,
	})
}

func (s *Server) runScan(r *http.Request) []network.ScanResult {
	q := r.URL.Query()
	ports := []int{config.DefaultScanPort}
	if raw := strings.TrimSpace(q.Get("ports")); raw != "" {
		parsed := []int{}
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			p, err := strconv.Atoi(part)
			if err != nil || p < 1 || p > 65535 {
				continue
			}
			parsed = append(parsed, p)
		}
		if len(parsed) > 0 {
			ports = parsed
		}
	}
	timeout := time.Duration(s.cfg.ScanTimeoutMs) * time.Millisecond
	if raw := strings.TrimSpace(q.Get("scanTimeoutMs")); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			timeout = time.Duration(ms) * time.Millisecond
		}
	}
	if timeout > MaxScanTimeout {
		timeout = MaxScanTimeout
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	nets, err := network.LocalSubnets()
	if err != nil || len(nets) == 0 {
		return nil
	}
	opts := network.DefaultOptions()
	opts.Ports = ports
	return network.Scan(ctx, nets, opts)
}

func (s *Server) handleRegisterPrinter(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Address string `json:"address"`
		Port    int    `json:"port"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeAPIError(w, printer.NewError(printer.CodeInvalidRequest, "", "cannot read request body"))
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPIError(w, printer.NewError(printer.CodeInvalidRequest, "", "invalid JSON body"))
		return
	}
	id, err := s.reg.AddNetworkPrinter(req.Name, req.Address, req.Port)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	p, err := s.reg.GetPrinter(id)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success":   true,
		"printerId": id,
		"printer":   p,
	})
}

// ---------- printer item ----------

// splitPrinterPath parses "/api/v1/printers/<escaped-id>[/<action>]".
func splitPrinterPath(p string) (id, action string, ok bool) {
	const prefix = "/api/v1/printers/"
	if !strings.HasPrefix(p, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(p, prefix)
	if rest == "" {
		return "", "", false
	}
	parts := strings.SplitN(rest, "/", 2)
	id, err := url.PathUnescape(parts[0])
	if err != nil || id == "" {
		return "", "", false
	}
	if len(parts) == 2 {
		action = parts[1]
		if action == "" || strings.Contains(action, "/") {
			return "", "", false
		}
	}
	return id, action, true
}

func (s *Server) handlePrinterItem(w http.ResponseWriter, r *http.Request) {
	id, action, ok := splitPrinterPath(r.URL.EscapedPath())
	if !ok {
		writeAPIError(w, printer.NewError(printer.CodeNotFound, "", "unknown endpoint %s", r.URL.Path))
		return
	}
	switch action {
	case "":
		switch r.Method {
		case http.MethodGet:
			p, err := s.reg.GetPrinter(id)
			if err != nil {
				writeAPIError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"printer": p})
		case http.MethodDelete:
			if err := s.reg.RemoveNetworkPrinter(id); err != nil {
				writeAPIError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "printerId": id})
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete)
		}
	case "connect":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		if err := s.reg.Connect(id); err != nil {
			writeAPIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true, "printerId": id, "connected": true,
		})
	case "disconnect":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		if err := s.reg.Disconnect(id); err != nil {
			writeAPIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true, "printerId": id, "connected": false,
		})
	case "status":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		st, err := s.reg.GetStatus(id)
		if err != nil {
			writeAPIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	case "print":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		data, perr := parsePrintBody(r)
		if perr != nil {
			perr.PrinterID = id
			writeAPIError(w, perr)
			return
		}
		if err := s.reg.Print(id, data); err != nil {
			writeAPIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true, "printerId": id,
		})
	default:
		writeAPIError(w, printer.NewError(printer.CodeNotFound, id, "unknown action %q", action))
	}
}

// parsePrintBody accepts either JSON {"data": "<base64>"} or a raw
// application/octet-stream body. Returned bytes are the exact print payload.
func parsePrintBody(r *http.Request) ([]byte, *printer.Error) {
	ct := r.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.TrimSpace(strings.ToLower(ct))

	body, err := io.ReadAll(io.LimitReader(r.Body, MaxPrintBytes+1))
	if err != nil {
		return nil, printer.NewError(printer.CodeInvalidRequest, "", "cannot read request body")
	}
	if len(body) > MaxPrintBytes {
		return nil, printer.NewError(printer.CodePayloadTooLarge, "", "payload exceeds %d bytes", MaxPrintBytes)
	}
	if ct == "application/octet-stream" {
		if len(body) == 0 {
			return nil, printer.NewError(printer.CodeInvalidRequest, "", "print payload must not be empty")
		}
		return body, nil
	}
	var req struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, printer.NewError(printer.CodeInvalidRequest, "", "invalid JSON body (want {\"data\": \"<base64>\"})")
	}
	if req.Data == "" {
		return nil, printer.NewError(printer.CodeInvalidRequest, "", "field \"data\" (base64) is required")
	}
	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		return nil, printer.NewError(printer.CodeInvalidRequest, "", "field \"data\" is not valid base64")
	}
	if len(data) == 0 {
		return nil, printer.NewError(printer.CodeInvalidRequest, "", "print payload must not be empty")
	}
	return data, nil
}

// ---------- JSON helpers ----------

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAPIError(w http.ResponseWriter, err error) {
	code := printer.CodeInternalError
	msg := "internal error"
	if perr, ok := printer.AsError(err); ok {
		code = perr.Code
		msg = perr.Message
	} else if err != nil {
		msg = err.Error()
	}
	writeJSON(w, statusFor(code), map[string]interface{}{
		"success": false,
		"error": map[string]string{
			"code":    code,
			"message": msg,
		},
	})
}

func writeMethodNotAllowed(w http.ResponseWriter, allow ...string) {
	w.Header().Set("Allow", strings.Join(allow, ", "))
	writeAPIError(w, printer.NewError(printer.CodeMethodNotAllowed, "", "method not allowed (allow: %s)", strings.Join(allow, ", ")))
}

// statusFor maps stable error codes to HTTP statuses.
func statusFor(code string) int {
	switch code {
	case printer.CodeInvalidRequest, printer.CodeInvalidPrinterID, printer.CodePayloadTooLarge:
		return http.StatusBadRequest
	case printer.CodeUnauthorized:
		return http.StatusUnauthorized
	case printer.CodeForbiddenOrigin:
		return http.StatusForbidden
	case printer.CodeNotFound, printer.CodePrinterNotFound, printer.CodeUSBDeviceNotFound:
		return http.StatusNotFound
	case printer.CodeMethodNotAllowed:
		return http.StatusMethodNotAllowed
	case printer.CodePrinterTimeout:
		return http.StatusGatewayTimeout
	case printer.CodePrinterConnFailed, printer.CodePrinterWriteFailed,
		printer.CodeUSBError, printer.CodeScanFailed:
		return http.StatusBadGateway
	case printer.CodeUSBUnsupported:
		return http.StatusNotImplemented
	case printer.CodePrinterBusy:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func parseBoolQuery(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func containsPrinter(printers []printer.Printer, id string) bool {
	for _, p := range printers {
		if p.ID == id {
			return true
		}
	}
	return false
}
