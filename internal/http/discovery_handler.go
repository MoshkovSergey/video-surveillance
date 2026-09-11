package httpapi

import (
	"net/http"
	"time"

	"gitverse.ru/cataclysm78/video-surveillance/internal/discovery"
)

const (
	// scanMaxTimeout — жёсткий предел длительности сканирования.
	scanMaxTimeout = 20 * time.Second
	// scanQuietPeriod — период тишины, означающий, что сеть ответила.
	scanQuietPeriod = 5 * time.Second
)

type discoveredDeviceDTO struct {
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
	Hardware     string   `json:"hardware"`
	XAddrs       []string `json:"xaddrs"`
	Added        bool     `json:"added"`
}

type scanResponse struct {
	Devices    []discoveredDeviceDTO `json:"devices"`
	DurationMs int64                 `json:"duration_ms"`
}

// handleDiscoveryScan выполняет WS-Discovery поиск ONVIF-камер в локальной сети.
func (h *Handler) handleDiscoveryScan(w http.ResponseWriter, r *http.Request) {
	devices, elapsed, err := discovery.Discover(r.Context(), scanMaxTimeout, scanQuietPeriod)
	if err != nil {
		h.logger.Error("discovery scan failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "scan failed: " + err.Error(),
		})
		return
	}

	// Отмечаем устройства, которые уже добавлены в систему.
	addedHosts := make(map[string]bool)
	if cams, err := h.cameraRepo.List(r.Context()); err == nil {
		for _, cam := range cams {
			if cam.ONVIF != nil {
				addedHosts[cam.ONVIF.Host] = true
			}
		}
	}

	dto := make([]discoveredDeviceDTO, 0, len(devices))
	for _, d := range devices {
		name := d.Name
		if name == "" {
			name = d.Model
		}
		if name == "" {
			name = d.Host
		}

		dto = append(dto, discoveredDeviceDTO{
			Host:         d.Host,
			Port:         d.Port,
			Name:         name,
			Manufacturer: d.Manufacturer,
			Model:        d.Model,
			Hardware:     d.Hardware,
			XAddrs:       d.XAddrs,
			Added:        addedHosts[d.Host],
		})
	}

	h.logger.Info("discovery scan completed",
		"devices", len(dto),
		"duration_ms", elapsed.Milliseconds(),
	)

	writeJSON(w, http.StatusOK, scanResponse{
		Devices:    dto,
		DurationMs: elapsed.Milliseconds(),
	})
}
