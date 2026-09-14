// Package network discovers LAN/TCP printers by probing for open raw-print
// ports (default 9100) on local subnets.
//
// Discovery is best-effort: a successful TCP connect means "something with
// an open port", which for port 9100 on a LAN is almost always a printer.
// Found hosts are reported with their address and port; naming a printer
// still happens via explicit registration (POST /api/v1/printers).
package network

import (
	"context"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

// ScanResult is one host with an open printer port.
type ScanResult struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

// Options tunes a scan.
type Options struct {
	Ports       []int
	DialTimeout time.Duration
	Workers     int
}

// DefaultOptions returns conservative scan settings.
func DefaultOptions() Options {
	return Options{
		Ports:       []int{9100},
		DialTimeout: 400 * time.Millisecond,
		Workers:     128,
	}
}

// LocalSubnets returns the /24 networks of all up interfaces, loopback
// included so tests and local virtual printers (127.0.0.1:9100 mocks)
// are discoverable.
func LocalSubnets() ([]*net.IPNet, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var out []*net.IPNet
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil {
				continue // IPv4 LANs only in v1
			}
			sub := &net.IPNet{IP: ip4.Mask(net.CIDRMask(24, 32)), Mask: net.CIDRMask(24, 32)}
			key := sub.String()
			if !seen[key] {
				seen[key] = true
				out = append(out, sub)
			}
		}
	}
	return out, nil
}

// Scan probes every host in nets for open ports. It stops early when ctx is
// cancelled and always returns the hits found so far, sorted.
func Scan(ctx context.Context, nets []*net.IPNet, opts Options) []ScanResult {
	if opts.Workers <= 0 {
		opts.Workers = 64
	}
	if opts.DialTimeout <= 0 {
		opts.DialTimeout = 400 * time.Millisecond
	}
	if len(opts.Ports) == 0 {
		opts.Ports = []int{9100}
	}

	type target struct {
		host string
		port int
	}
	jobs := make(chan target)
	results := make(chan ScanResult)

	var wg sync.WaitGroup
	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				addr := net.JoinHostPort(t.host, strconv.Itoa(t.port))
				conn, err := net.DialTimeout("tcp", addr, opts.DialTimeout)
				if err != nil {
					continue
				}
				_ = conn.Close()
				select {
				case results <- ScanResult{Address: t.host, Port: t.port}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, n := range nets {
			for _, host := range hostsOf(n) {
				for _, port := range opts.Ports {
					select {
					case jobs <- target{host: host, port: port}:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var out []ScanResult
	for r := range results {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Address != out[j].Address {
			return out[i].Address < out[j].Address
		}
		return out[i].Port < out[j].Port
	})
	return out
}

// hostsOf expands a /24 into its 254 usable host addresses.
func hostsOf(n *net.IPNet) []string {
	base := n.IP.To4()
	if base == nil {
		return nil
	}
	out := make([]string, 0, 254)
	for i := 1; i <= 254; i++ {
		ip := net.IPv4(base[0], base[1], base[2], byte(i))
		out = append(out, ip.String())
	}
	return out
}
