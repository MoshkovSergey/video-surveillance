package discovery

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Device — найденное в сети ONVIF-устройство.
type Device struct {
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
	Hardware     string   `json:"hardware"`
	XAddrs       []string `json:"xaddrs"`
}

const (
	multicastAddr = "239.255.255.250"
	multicastPort = 3702
	readSlice     = 500 * time.Millisecond
	// minScan — минимальное время ожидания ответов: даём устройствам шанс.
	minScan = 2 * time.Second
	// pollInterval — период проверки появления новых устройств.
	pollInterval = 250 * time.Millisecond
)

// probeEnvelope формирует WS-Discovery Probe для ONVIF-устройств.
func probeEnvelope() string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" ` +
		`xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing" ` +
		`xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" ` +
		`xmlns:dn="http://www.onvif.org/ver10/network/wsdl">` +
		`<s:Header>` +
		`<a:Action s:mustUnderstand="1">http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</a:Action>` +
		`<a:MessageID>uuid:` + uuid.NewString() + `</a:MessageID>` +
		`<a:ReplyTo><a:Address>http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous</a:Address></a:ReplyTo>` +
		`<a:To s:mustUnderstand="1">urn:schemas-xmlsoap-org:ws:2005:04:discovery</a:To>` +
		`</s:Header>` +
		`<s:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></s:Body>` +
		`</s:Envelope>`
}

// Discover рассылает WS-Discovery Probe по всем активным IPv4-интерфейсам
// и собирает ответы ONVIF-устройств.
//
// Сканирование завершается досрочно, когда сеть «ответила»: новые устройства
// не появляются в течение quietPeriod (но не раньше minScan).
// Жёсткий предел длительности — maxTimeout.
// Возвращает найденные устройства и фактическую длительность сканирования.
func Discover(ctx context.Context, maxTimeout, quietPeriod time.Duration) ([]Device, time.Duration, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, 0, fmt.Errorf("list interfaces: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, maxTimeout)
	defer cancel()

	var mu sync.Mutex
	seen := make(map[string]*Device)
	var wg sync.WaitGroup

	probe := []byte(probeEnvelope())
	dst := &net.UDPAddr{IP: net.ParseIP(multicastAddr), Port: multicastPort}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 ||
			iface.Flags&net.FlagMulticast == 0 ||
			iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		conn, err := net.ListenMulticastUDP("udp4", &iface,
			&net.UDPAddr{IP: net.ParseIP(multicastAddr), Port: multicastPort})
		if err != nil {
			continue
		}

		wg.Add(1)
		go func(c *net.UDPConn) {
			defer wg.Done()
			defer c.Close()

			_ = c.SetReadBuffer(64 * 1024)

			if _, err := c.WriteToUDP(probe, dst); err != nil {
				return
			}

			buf := make([]byte, 65535)
			for {
				_ = c.SetReadDeadline(time.Now().Add(readSlice))

				n, _, err := c.ReadFromUDP(buf)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						continue
					}
					return
				}

				dev, ok := parseProbeMatch(buf[:n])
				if !ok {
					continue
				}

				mu.Lock()
				key := fmt.Sprintf("%s:%d", dev.Host, dev.Port)
				if old, exists := seen[key]; exists {
					mergeDevice(old, dev)
				} else {
					seen[key] = dev
				}
				mu.Unlock()
			}
		}(conn)
	}

	start := time.Now()
	lastChange := start
	lastCount := 0

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

waitLoop:
	for {
		select {
		case <-ctx.Done():
			// Исчерпан жёсткий лимит maxTimeout.
			break waitLoop
		case <-ticker.C:
			mu.Lock()
			n := len(seen)
			mu.Unlock()

			if n != lastCount {
				lastCount = n
				lastChange = time.Now()
			}

			// Сеть «ответила»: новых устройств нет уже quietPeriod.
			if time.Since(start) >= minScan && time.Since(lastChange) >= quietPeriod {
				break waitLoop
			}
		}
	}

	// Останавливаем горутины приёма и дожидаемся их завершения.
	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}

	elapsed := time.Since(start)

	mu.Lock()
	devices := make([]Device, 0, len(seen))
	for _, d := range seen {
		devices = append(devices, *d)
	}
	mu.Unlock()

	return devices, elapsed, nil
}

// parseProbeMatch извлекает данные устройства из ProbeMatch.
func parseProbeMatch(data []byte) (*Device, bool) {
	decoder := xml.NewDecoder(bytes.NewReader(data))

	var xaddrs, scopes strings.Builder
	capture := ""

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false
		}

		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "XAddrs":
				capture = "x"
				xaddrs.Reset()
			case "Scopes":
				capture = "s"
				scopes.Reset()
			}
		case xml.CharData:
			switch capture {
			case "x":
				xaddrs.Write(el)
			case "s":
				scopes.Write(el)
			}
		case xml.EndElement:
			switch el.Name.Local {
			case "XAddrs", "Scopes":
				capture = ""
			}
		}
	}

	fields := strings.Fields(xaddrs.String())
	if len(fields) == 0 {
		return nil, false
	}

	u, err := url.Parse(fields[0])
	if err != nil || u.Host == "" {
		return nil, false
	}

	port := 80
	if p := u.Port(); p != "" {
		_, _ = fmt.Sscanf(p, "%d", &port)
	}

	dev := &Device{
		Host:   u.Hostname(),
		Port:   port,
		XAddrs: fields,
	}

	for _, sc := range strings.Fields(scopes.String()) {
		lower := strings.ToLower(sc)
		value := scopeValue(sc)

		switch {
		case strings.Contains(lower, "/name/"):
			dev.Name = value
		case strings.Contains(lower, "/manufacturer/"):
			dev.Manufacturer = value
		case strings.Contains(lower, "/model/"):
			dev.Model = value
		case strings.Contains(lower, "/hardware/"):
			dev.Hardware = value
		}
	}

	return dev, true
}

// scopeValue возвращает последний сегмент scope-URI в раскодированном виде.
func scopeValue(scope string) string {
	i := strings.LastIndex(scope, "/")
	if i < 0 || i+1 >= len(scope) {
		return ""
	}
	v, err := url.PathUnescape(scope[i+1:])
	if err != nil {
		return scope[i+1:]
	}
	return v
}

// mergeDevice объединяет дублирующиеся ответы одного устройства.
func mergeDevice(dst, src *Device) {
	if dst.Name == "" {
		dst.Name = src.Name
	}
	if dst.Manufacturer == "" {
		dst.Manufacturer = src.Manufacturer
	}
	if dst.Model == "" {
		dst.Model = src.Model
	}
	if dst.Hardware == "" {
		dst.Hardware = src.Hardware
	}
	dst.XAddrs = append(dst.XAddrs, src.XAddrs...)
}