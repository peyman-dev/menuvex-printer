package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sinaqaffari/coffish/desktop/internal/printer"
)

// PrinterSchemaVersion is stored in the local database meta section. Version 2
// infers a connection kind and station role for printers created before the
// unified printer centre existed, including prefixed "LAN 192.168.x.x:9100" strings.
const PrinterSchemaVersion = "2"

// PrinterStats summarizes the printer inventory for the management centre.
type PrinterStats struct {
	Total, Active, Network, Spooler, USB, Legacy int
}

// MigratePrinterSchema is an idempotent, versioned data migration. It only
// fills fields that are missing; it never rewrites operator-visible values.
func (s *Store) MigratePrinterSchema(ctx context.Context) error {
	return s.withTx(ctx, func(st *persistedDB) error {
		if st.Entities == nil {
			return nil
		}
		if st.Meta == nil {
			st.Meta = map[string]string{}
		}
		if st.Meta["printer_schema"] == PrinterSchemaVersion {
			return nil
		}
		now := time.Now().UTC().Format(time.RFC3339)
		for key, e := range st.Entities {
			if e.Type != "printer" || e.Deleted {
				continue
			}
			payload := map[string]any{}
			if err := json.Unmarshal(e.Payload, &payload); err != nil {
				continue
			}
			ent := Entity{ID: e.ID, Type: e.Type, Version: e.Version, Payload: e.Payload}
			changed := false
			if fmt.Sprint(payload["kind"]) == "" || payload["kind"] == nil {
				if kind, host, port, queue, usbPath := inferLegacyConnection(ent); kind != "" {
					payload["kind"] = kind
					switch kind {
					case printer.KindNetwork:
						payload["host"] = host
						payload["port"] = strconv.Itoa(port)
					case printer.KindSpooler:
						payload["queue_name"] = queue
					case printer.KindUSB:
						payload["usb_path"] = usbPath
					}
					changed = true
				}
			}
			if fmt.Sprint(payload["role"]) == "" || payload["role"] == nil {
				if r := inferRoleFromStation(fmt.Sprint(payload["station"])); r != "" {
					payload["role"] = r
					changed = true
				}
			}
			if fmt.Sprint(payload["paper_width"]) == "" || payload["paper_width"] == nil {
				payload["paper_width"] = "80mm"
				changed = true
			}
			if fmt.Sprint(payload["width_dots"]) == "" || payload["width_dots"] == nil {
				paper := fmt.Sprint(payload["paper_width"])
				width := 576
				if paper == "58mm" {
					width = 384
				}
				payload["width_dots"] = strconv.Itoa(width)
				changed = true
			}
			if _, ok := payload["cut"]; !ok {
				payload["cut"] = true
				changed = true
			}
			if _, ok := payload["is_active"]; !ok {
				payload["is_active"] = true
				changed = true
			}
			if !changed {
				continue
			}
			payload["migrated_at"] = now
			raw, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			e.Payload = raw
			e.Version++
			payload["version"] = e.Version
			if raw, err = json.Marshal(payload); err == nil {
				e.Payload = raw
			}
			st.Entities[key] = e
		}
		st.Meta["printer_schema"] = PrinterSchemaVersion
		return nil
	})
}

// Printers returns every saved printer, newest UI fields included, sorted by
// name. Legacy records keep an empty Kind and are printed through the legacy
// path.
func (s *Store) Printers(ctx context.Context) ([]printer.Profile, error) {
	ents, err := s.Entities(ctx, "printer")
	if err != nil {
		return nil, err
	}
	out := make([]printer.Profile, 0, len(ents))
	for _, e := range ents {
		out = append(out, ProfileFromEntity(e))
	}
	sortProfiles(out)
	return out, nil
}

// Printer returns one printer profile.
func (s *Store) Printer(ctx context.Context, id string) (printer.Profile, error) {
	e, err := s.Entity(ctx, "printer", id)
	if err != nil {
		return printer.Profile{}, errors.New("چاپگر مورد نظر یافت نشد")
	}
	return ProfileFromEntity(e), nil
}

// SavePrinter validates and upserts one printer. Connection identity is unique:
// saving the same network target or OS queue twice is rejected instead of
// silently creating a duplicate that would print every receipt twice.
func (s *Store) SavePrinter(ctx context.Context, p printer.Profile) (string, error) {
	p = p.Normalize()
	if err := p.Validate(); err != nil {
		return "", err
	}
	existing, err := s.Printers(ctx)
	if err != nil {
		return "", err
	}
	if p.ID == "" && len(existing) >= printer.MaxPrinters {
		return "", fmt.Errorf("حداکثر %d چاپگر قابل تعریف است", printer.MaxPrinters)
	}
	key := p.ConnectionKey()
	for _, other := range existing {
		if other.ID == p.ID {
			continue
		}
		if key != "" && other.ConnectionKey() == key {
			return "", fmt.Errorf("چاپگری با همین مقصد (%s) قبلاً ثبت شده است: «%s»", other.ConnectionDetail(), other.Name)
		}
	}
	fields := map[string]any{
		"title":            p.Name,
		"name":             p.Name,
		"model":            p.Model,
		"station":          p.Station,
		"role":             p.Role,
		"kind":             p.Kind,
		"host":             p.Host,
		"port":             strconv.Itoa(p.Port),
		"queue_name":       p.QueueName,
		"queue":            p.QueueName,
		"usb_path":         p.USB.DevicePath,
		"usb_vid":          p.USB.VendorID,
		"usb_pid":          p.USB.ProductID,
		"usb_serial":       p.USB.Serial,
		"usb_bus":          p.USB.Bus,
		"usb_port":         p.USB.PortPath,
		"usb_manufacturer": p.USB.Manufacturer,
		"usb_product":      p.USB.Product,
		"paper_width":      paperWidthLabel(p.PaperMM),
		"width_dots":       strconv.Itoa(p.WidthDots),
		"copies":           strconv.Itoa(p.Copies),
		"font_family":      p.FontFamily,
		"font_size":        strconv.Itoa(p.FontSize),
		"cut":              p.Cut,
		"is_active":        p.Active,
	}
	switch p.Kind {
	case printer.KindNetwork:
		fields["connection"] = fmt.Sprintf("%s:%d", p.Host, p.Port)
	case printer.KindSpooler:
		fields["connection"] = ""
	case printer.KindUSB:
		fields["connection"] = ""
	}
	return s.SaveLocalRecord(ctx, "printer", p.ID, fields)
}

// SetPrinterActive toggles one printer without deleting its configuration.
func (s *Store) SetPrinterActive(ctx context.Context, id string, active bool) error {
	if _, err := s.Printer(ctx, id); err != nil {
		return err
	}
	_, err := s.SaveLocalRecord(ctx, "printer", id, map[string]any{"is_active": active})
	return err
}

// DeletePrinter removes a printer and any category routes that pointed at it.
// A printer with an in-flight job is never deleted underneath the worker.
func (s *Store) DeletePrinter(ctx context.Context, id string) error {
	if _, err := s.Printer(ctx, id); err != nil {
		return err
	}
	jobs, err := s.PrintJobs(ctx, "")
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.PrinterID == id && j.Status == PrintStatusPrinting {
			return errors.New("این چاپگر یک Job در حال چاپ دارد؛ چند لحظه بعد دوباره تلاش کنید")
		}
	}
	routes, _ := s.Entities(ctx, "printer_route")
	for _, r := range routes {
		if strings.TrimSpace(r.Field("printer_id")) == id {
			_ = s.DeleteLocalRecord(ctx, "printer_route", r.ID)
		}
	}
	return s.DeleteLocalRecord(ctx, "printer", id)
}

// SavePrinterCheck stores the latest verification result on the printer so the
// management centre shows evidence rather than a guess after a restart.
func (s *Store) SavePrinterCheck(ctx context.Context, id, state, detail, stages, at string) error {
	if _, err := s.Printer(ctx, id); err != nil {
		return err
	}
	if at == "" {
		at = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.SaveLocalRecord(ctx, "printer", id, map[string]any{
		"last_check_at":     at,
		"last_check_state":  state,
		"last_check_detail": detail,
		"last_check_stages": stages,
	})
	return err
}

// PrinterStats counts printers by connection kind.
func (s *Store) PrinterStats(ctx context.Context) PrinterStats {
	list, _ := s.Printers(ctx)
	var st PrinterStats
	for _, p := range list {
		st.Total++
		if p.Active {
			st.Active++
		}
		switch p.Kind {
		case printer.KindNetwork:
			st.Network++
		case printer.KindSpooler:
			st.Spooler++
		case printer.KindUSB:
			st.USB++
		default:
			st.Legacy++
		}
	}
	return st
}

// ProfileFromEntity maps the durable entity fields to the printer model. It is
// intentionally permissive: old records remain readable and editable.
func ProfileFromEntity(e Entity) printer.Profile {
	p := printer.Profile{
		ID:        e.ID,
		Name:      strings.TrimSpace(e.Label()),
		Model:     e.Field("model"),
		Station:   e.Field("station"),
		Role:      strings.ToLower(e.Field("role")),
		Kind:      strings.ToLower(e.Field("kind")),
		Host:      e.Field("host"),
		QueueName: firstNonEmpty(e.Field("queue_name"), e.Field("queue")),
		CreatedAt: e.Field("created_at"),
		UpdatedAt: e.Field("updated_at"),
	}
	p.Port, _ = strconv.Atoi(e.Field("port"))
	if p.Kind == "" {
		kind, host, port, queue, usbPath := inferLegacyConnection(e)
		p.Kind, p.Host, p.Port, p.QueueName = kind, host, port, queue
		p.USB.DevicePath = usbPath
	}
	if p.Kind == printer.KindNetwork && p.Port == 0 {
		if _, port, ok := splitHostPort(e.Field("connection")); ok {
			p.Port = port
		}
	}
	p.USB.VendorID = e.Field("usb_vid")
	p.USB.ProductID = e.Field("usb_pid")
	p.USB.Serial = e.Field("usb_serial")
	p.USB.Bus = e.Field("usb_bus")
	p.USB.PortPath = e.Field("usb_port")
	p.USB.Manufacturer = e.Field("usb_manufacturer")
	p.USB.Product = e.Field("usb_product")
	p.USB.System = e.Field("system")
	switch e.Field("paper_width") {
	case "58mm":
		p.PaperMM = printer.Paper58
	case "80mm":
		p.PaperMM = printer.Paper80
	}
	p.WidthDots, _ = strconv.Atoi(e.Field("width_dots"))
	p.Copies, _ = strconv.Atoi(e.Field("copies"))
	p.FontFamily = e.Field("font_family")
	p.FontSize, _ = strconv.Atoi(e.Field("font_size"))
	if p.Role == "" {
		p.Role = inferRoleFromStation(p.Station)
	}
	p.Cut = e.Field("cut") != "false"
	p.Active = e.Field("is_active") != "false"
	p.LastCheckAt = e.Field("last_check_at")
	p.LastCheckState = e.Field("last_check_state")
	p.LastCheckDetail = e.Field("last_check_detail")
	if raw := e.Field("last_check_stages"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &p.LastCheckStages)
	}
	return p.Normalize()
}

func inferRoleFromStation(station string) string {
	s := strings.ToLower(strings.TrimSpace(station))
	if s == "" {
		return ""
	}
	hasCashier := strings.Contains(s, "صندوق") || strings.Contains(s, "cashier")
	hasKitchen := strings.Contains(s, "بار") || strings.Contains(s, "آشپزخانه") || strings.Contains(s, "kitchen")
	if hasCashier && hasKitchen {
		return "all"
	}
	if hasCashier {
		return "cashier"
	}
	if hasKitchen {
		return "kitchen"
	}
	return ""
}

func inferLegacyConnection(e Entity) (kind, host string, port int, queue, usbPath string) {
	connection := strings.TrimSpace(e.Field("connection"))
	queue = strings.TrimSpace(e.Field("queue"))
	if h, p, ok := splitHostPort(connection); ok {
		if printer.ValidateNetworkTarget(h, p) == nil {
			return printer.KindNetwork, h, p, queue, ""
		}
	}
	if queue != "" {
		return printer.KindSpooler, "", 0, queue, ""
	}
	if strings.HasPrefix(connection, "/dev/") {
		return printer.KindUSB, "", 0, "", connection
	}
	return "", "", 0, "", ""
}

func splitHostPort(raw string) (string, int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0, false
	}
	// Handle prefixed legacy strings like "USB / LAN 192.168.1.201:9100" or "LAN 192.168.1.202:9100"
	for _, token := range strings.Fields(raw) {
		idx := strings.LastIndex(token, ":")
		if idx <= 0 || idx == len(token)-1 {
			continue
		}
		host := strings.Trim(token[:idx], "[]")
		port, err := strconv.Atoi(token[idx+1:])
		if err != nil || port < 1 || port > 65535 {
			continue
		}
		return host, port, true
	}
	return "", 0, false
}

func sortProfiles(list []printer.Profile) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].Name == list[j].Name {
			return list[i].ID < list[j].ID
		}
		return list[i].Name < list[j].Name
	})
}

func paperWidthLabel(mm int) string {
	if mm == printer.Paper58 {
		return "58mm"
	}
	return "80mm"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
