package httpapi

import (
	"net/http"
	"strings"

	"gitverse.ru/cataclysm78/video-surveillance/internal/onvif"
)

type onvifProbeRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type onvifProfileDTO struct {
	Token     string `json:"token"`
	Name      string `json:"name"`
	StreamURI string `json:"stream_uri"`
}

// handleOnvifProbe опрашивает ONVIF-камеру и возвращает профили с RTSP-адресами.
func (h *Handler) handleOnvifProbe(w http.ResponseWriter, r *http.Request) {
	var req onvifProbeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректное тело запроса"})
		return
	}

	host := strings.TrimSpace(req.Host)
	if host == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "укажите IP-адрес камеры"})
		return
	}

	port := req.Port
	if port == 0 {
		port = 80
	}

	creds := onvif.Credentials{Username: req.Username, Password: req.Password}

	profiles, err := onvif.GetProfiles(r.Context(), host, port, creds)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ONVIF: " + err.Error()})
		return
	}
	if len(profiles) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ONVIF: профили не найдены"})
		return
	}
	if len(profiles) > 8 {
		profiles = profiles[:8]
	}

	dto := make([]onvifProfileDTO, 0, len(profiles))
	for _, p := range profiles {
		item := onvifProfileDTO{Token: p.Token, Name: p.Name}
		if uri, err := onvif.GetStreamUri(r.Context(), host, port, creds, p.Token); err == nil {
			item.StreamURI = uri
		}
		dto = append(dto, item)
	}

	writeJSON(w, http.StatusOK, dto)
}