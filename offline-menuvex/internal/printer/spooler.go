package printer

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
)

// QueueDetail holds enriched metadata for an installed OS print queue across
// Windows (WinSpool), Linux (CUPS), and macOS (CUPS).
type QueueDetail struct {
	QueueName    string `json:"queueName"`
	DeviceURI    string `json:"deviceUri,omitempty"`
	DriverModel  string `json:"driverModel,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	PortName     string `json:"portName,omitempty"`
	Category     string `json:"category"` // "usb" | "lan"
	PaperMM      int    `json:"paperMm"`
}

// ParseQueueList normalizes installed-queue output (lpstat -e / Get-Printer)
// and removes duplicates while dropping unsafe names.
func ParseQueueList(raw string) []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || seen[name] || ValidateQueueName(name) != nil {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ParseCUPSDevices extracts queue -> device URI from `lpstat -v` output across
// locales (e.g. "device for POS-80: usb://EPSON/TM-T20III?serial=123").
func ParseCUPSDevices(raw string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, ": ")
		if idx <= 0 {
			continue
		}
		prefix := strings.TrimSpace(line[:idx])
		uri := strings.TrimSpace(line[idx+2:])
		if uri == "" {
			continue
		}
		fields := strings.Fields(prefix)
		if len(fields) == 0 {
			continue
		}
		queue := fields[len(fields)-1]
		if ValidateQueueName(queue) == nil {
			out[queue] = uri
		}
	}
	return out
}

// EnrichQueueDetails returns rich specifications for each queue in `queues`.
func EnrichQueueDetails(ctx context.Context, queues []string) map[string]QueueDetail {
	rawDetails := platformQueueDetails(ctx)
	out := make(map[string]QueueDetail, len(queues))
	for _, q := range queues {
		d, ok := rawDetails[q]
		if !ok {
			d = QueueDetail{QueueName: q}
		}
		mfg, model, cat, portInfo, paper := InferPrinterSpecs(q, d.DeviceURI, d.DriverModel, d.PortName)
		if d.Manufacturer == "" {
			d.Manufacturer = mfg
		}
		if d.DriverModel == "" {
			d.DriverModel = model
		}
		if d.PortName == "" {
			d.PortName = portInfo
		}
		if d.Category == "" {
			d.Category = cat
		}
		if d.PaperMM == 0 {
			d.PaperMM = paper
		}
		out[q] = d
	}
	return out
}

// InferPrinterSpecs extracts human-friendly printer specifications from queue
// names, CUPS URIs, or Windows driver/port strings.
func InferPrinterSpecs(name, uri, driver, portName string) (manufacturer, model, category, portInfo string, paperMM int) {
	combined := strings.ToUpper(name + " " + uri + " " + driver + " " + portName)
	paperMM = Paper80
	if strings.Contains(combined, "58") || strings.Contains(combined, "POS58") || strings.Contains(combined, "POS-58") || strings.Contains(combined, "TM-T88-58") {
		paperMM = Paper58
	}

	switch {
	case strings.Contains(combined, "EPSON") || strings.Contains(combined, "04B8"):
		manufacturer = "Epson"
	case strings.Contains(combined, "BIXOLON") || strings.Contains(combined, "1504") || strings.Contains(combined, "0419") || strings.Contains(combined, "SRP-"):
		manufacturer = "Bixolon"
	case strings.Contains(combined, "STAR") || strings.Contains(combined, "0519") || strings.Contains(combined, "TSP"):
		manufacturer = "Star Micronics"
	case strings.Contains(combined, "XPRINTER") || strings.Contains(combined, "XP-"):
		manufacturer = "Xprinter"
	case strings.Contains(combined, "CITIZEN") || strings.Contains(combined, "CT-"):
		manufacturer = "Citizen"
	case strings.Contains(combined, "RONGTA") || strings.Contains(combined, "RP80") || strings.Contains(combined, "RP58"):
		manufacturer = "Rongta"
	case strings.Contains(combined, "SEWOO") || strings.Contains(combined, "SLK"):
		manufacturer = "Sewoo"
	case strings.Contains(combined, "HPRT") || strings.Contains(combined, "TP80"):
		manufacturer = "HPRT"
	case strings.Contains(combined, "CUSTOM"):
		manufacturer = "Custom"
	default:
		manufacturer = "Thermal POS"
	}

	model = strings.TrimSpace(driver)
	if model == "" && strings.HasPrefix(strings.ToLower(uri), "usb://") {
		trimmed := strings.TrimPrefix(uri, "usb://")
		trimmed = strings.TrimPrefix(trimmed, "USB://")
		if qIdx := strings.Index(trimmed, "?"); qIdx > 0 {
			trimmed = trimmed[:qIdx]
		}
		trimmed = strings.ReplaceAll(trimmed, "/", " ")
		trimmed = strings.ReplaceAll(trimmed, "%20", " ")
		model = strings.TrimSpace(trimmed)
	}
	if model == "" {
		model = strings.ReplaceAll(name, "_", " ")
	}

	lowerURI := strings.ToLower(uri)
	upperPort := strings.ToUpper(portName)
	switch {
	case strings.HasPrefix(lowerURI, "socket://") || strings.HasPrefix(lowerURI, "ipp://") || strings.HasPrefix(lowerURI, "lpd://") || strings.HasPrefix(lowerURI, "http://") ||
		strings.HasPrefix(upperPort, "IP_") || strings.Contains(upperPort, "TCP") || strings.Contains(upperPort, "9100"):
		category = "lan"
		if uri != "" {
			portInfo = uri
		} else if portName != "" {
			portInfo = portName
		} else {
			portInfo = "درگاه شبکه (LAN)"
		}
	default:
		category = "usb"
		if strings.HasPrefix(lowerURI, "usb://") {
			portInfo = "درگاه USB سیستم‌عامل (" + uri + ")"
		} else if portName != "" {
			portInfo = "درگاه " + portName
		} else {
			portInfo = "صف چاپ سیستم‌عامل (" + osDisplayName() + ")"
		}
	}
	return manufacturer, model, category, portInfo, paperMM
}

func osDisplayName() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows Spooler"
	case "darwin":
		return "macOS CUPS"
	case "linux":
		return "Linux CUPS / USB"
	default:
		return runtime.GOOS
	}
}

// BuildDiscoveredPrinters creates a unified slice of DiscoveredPrinter cards
// from USB devices, OS print queues, and LAN scan results, marking any printer
// that is already saved in `saved`.
func BuildDiscoveredPrinters(
	queues []string,
	queueDetails map[string]QueueDetail,
	usbDevices []USBDevice,
	scan *ScanStatus,
	saved []Profile,
) []DiscoveredPrinter {
	savedByKey := map[string]Profile{}
	for _, p := range saved {
		if k := p.ConnectionKey(); k != "" {
			savedByKey[k] = p
		}
	}

	var out []DiscoveredPrinter
	seenKeys := map[string]bool{}

	// 1. Direct USB devices
	for _, d := range usbDevices {
		mfg, model, _, _, paper := InferPrinterSpecs(d.DisplayName(), "", d.Product, d.DevicePath)
		if strings.TrimSpace(d.Manufacturer) != "" {
			mfg = strings.TrimSpace(d.Manufacturer)
		}
		if strings.TrimSpace(d.Product) != "" {
			model = strings.TrimSpace(d.Product)
		}
		vidPid := ""
		if d.VendorID != "" && d.ProductID != "" {
			vidPid = fmt.Sprintf("%s:%s", strings.ToUpper(d.VendorID), strings.ToUpper(d.ProductID))
		}
		connDetail := vidPid
		if d.DevicePath != "" {
			if connDetail != "" {
				connDetail = d.DevicePath + " • " + connDetail
			} else {
				connDetail = d.DevicePath
			}
		}
		portInfo := "USB مستقیم"
		if d.Bus != "" || d.PortPath != "" {
			portInfo = fmt.Sprintf("Bus %s • Port %s", d.Bus, d.PortPath)
		}
		statusLabel := "آماده اتصال مستقیم USB"
		statusClass := "status-ok"
		if !d.Accessible {
			statusLabel = "نیازمند صف سیستم‌عامل (بدون گره usblp)"
			statusClass = "status-warn"
		}

		// Check if there is a matching OS queue for non-accessible direct USB device
		matchedQueue := ""
		if !d.Accessible {
			for _, q := range queues {
				if strings.Contains(strings.ToLower(q), strings.ToLower(d.Product)) && d.Product != "" {
					matchedQueue = q
					break
				}
			}
		}

		card := DiscoveredPrinter{
			Kind:             KindUSB,
			Category:         "usb",
			TypeBadge:        "پرینتر USB",
			Name:             d.DisplayName(),
			Model:            model,
			Manufacturer:     mfg,
			ConnectionDetail: connDetail,
			PortInfo:         portInfo,
			Protocol:         "ESC/POS • USB Printer Class",
			SystemInfo:       osDisplayName(),
			PaperMM:          paper,
			WidthDots:        DefaultWidthDots(paper),
			QueueName:        matchedQueue,
			USB:              d,
			Accessible:       d.Accessible,
			StatusLabel:      statusLabel,
			StatusClass:      statusClass,
			AccessNote:       d.AccessNote,
		}
		key := d.Identity()
		if sp, ok := savedByKey[key]; ok {
			card.AlreadySaved = true
			card.SavedPrinterID = sp.ID
			card.SavedStation = sp.Name
		}
		seenKeys[key] = true
		out = append(out, card)
	}

	// 2. Installed OS Print Queues (USB / Spooler / Driver)
	for _, q := range queues {
		detail, ok := queueDetails[q]
		if !ok {
			mfg, model, cat, portInfo, paper := InferPrinterSpecs(q, "", "", "")
			detail = QueueDetail{
				QueueName:    q,
				Manufacturer: mfg,
				DriverModel:  model,
				PortName:     portInfo,
				Category:     cat,
				PaperMM:      paper,
			}
		}
		paper := detail.PaperMM
		if paper != Paper58 && paper != Paper80 {
			paper = Paper80
		}
		typeBadge := "پرینتر USB / سیستم‌عامل"
		if detail.Category == "lan" {
			typeBadge = "پرینتر شبکه (نصب‌شده)"
		}
		card := DiscoveredPrinter{
			Kind:             KindSpooler,
			Category:         detail.Category,
			TypeBadge:        typeBadge,
			Name:             q,
			Model:            detail.DriverModel,
			Manufacturer:     detail.Manufacturer,
			ConnectionDetail: q,
			PortInfo:         detail.PortName,
			Protocol:         "ESC/POS RAW • Spooler",
			SystemInfo:       osDisplayName(),
			PaperMM:          paper,
			WidthDots:        DefaultWidthDots(paper),
			QueueName:        q,
			Accessible:       true,
			StatusLabel:      "نصب‌شده و آماده انتخاب",
			StatusClass:      "status-ok",
			AccessNote:       "صف چاپ سیستم‌عامل",
		}
		key := "spooler|" + strings.ToLower(q)
		if sp, ok := savedByKey[key]; ok {
			card.AlreadySaved = true
			card.SavedPrinterID = sp.ID
			card.SavedStation = sp.Name
		}
		if !seenKeys[key] {
			seenKeys[key] = true
			out = append(out, card)
		}
	}

	// 3. LAN Network Scan Results
	if scan != nil {
		for _, r := range scan.Results {
			port := r.Port
			if port == 0 {
				port = DefaultNetworkPort
			}
			connDetail := fmt.Sprintf("%s:%d", r.Host, port)
			card := DiscoveredPrinter{
				Kind:             KindNetwork,
				Category:         "lan",
				TypeBadge:        "پرینتر شبکه (LAN)",
				Name:             fmt.Sprintf("پرینتر شبکه %s", r.Host),
				Model:            "Thermal Receipt Printer (LAN)",
				Manufacturer:     "ESC/POS Network",
				ConnectionDetail: connDetail,
				PortInfo:         fmt.Sprintf("TCP Port %d • %s", port, scan.CIDR),
				Protocol:         "ESC/POS RAW (JetDirect)",
				SystemInfo:       "شبکه محلی (LAN)",
				PaperMM:          Paper80,
				WidthDots:        DefaultWidthDots(Paper80),
				Host:             r.Host,
				Port:             port,
				Accessible:       true,
				StatusLabel:      "پورت باز است • آماده اتصال",
				StatusClass:      "status-ok",
				AccessNote:       fmt.Sprintf("میزبان %s روی پورت %d پاسخ داد", r.Host, port),
			}
			key := fmt.Sprintf("network|%s|%d", strings.ToLower(r.Host), port)
			if sp, ok := savedByKey[key]; ok {
				card.AlreadySaved = true
				card.SavedPrinterID = sp.ID
				card.SavedStation = sp.Name
			}
			if !seenKeys[key] {
				seenKeys[key] = true
				out = append(out, card)
			}
		}
	}

	return out
}
