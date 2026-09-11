package onvif

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// EventMessage — сообщение уведомления ONVIF (PullPoint).
type EventMessage struct {
	Time  time.Time
	Topic string
	Items map[string]string
}

var eventPaths = []string{"/onvif/event_service", "/onvif/events", "/event_service"}

const pullTimeout = 5 * time.Second

// Subscribe создает PullPoint-подписку на события камеры.
// Возвращает URL подписки для последующих запросов PullMessages.
func Subscribe(ctx context.Context, host string, port int, creds Credentials) (string, error) {
	body := "<tev:CreatePullPointSubscription>" +
		"<tev:InitialTerminationTime>PT5M</tev:InitialTerminationTime>" +
		"</tev:CreatePullPointSubscription>"

	ctx, cancel := context.WithTimeout(ctx, requestTimeout*2)
	defer cancel()

	var lastErr error
	for _, p := range eventPaths {
		u := fmt.Sprintf("http://%s:%d%s", host, port, p)

		data, err := soapEvent(ctx, u, creds, body, true)
		if err != nil {
			data, err = soapEvent(ctx, u, creds, body, false)
		}
		if err != nil {
			lastErr = err
			continue
		}

		addr := firstElementText(data, "Address")
		if addr == "" {
			lastErr = fmt.Errorf("onvif events %s: subscription address not found", u)
			continue
		}
		return addr, nil
	}
	return "", lastErr
}

// PullMessages выполняет long-poll запрос сообщений подписки.
func PullMessages(ctx context.Context, subURL string, creds Credentials) ([]EventMessage, error) {
	body := "<tev:PullMessages>" +
		"<tev:Timeout>PT5S</tev:Timeout>" +
		"<tev:MessageLimit>32</tev:MessageLimit>" +
		"</tev:PullMessages>"

	ctx, cancel := context.WithTimeout(ctx, pullTimeout+requestTimeout)
	defer cancel()

	data, err := soapEvent(ctx, subURL, creds, body, true)
	if err != nil {
		data, err = soapEvent(ctx, subURL, creds, body, false)
	}
	if err != nil {
		return nil, err
	}
	return parseEvents(data), nil
}

// soapEvent — SOAP-запрос к сервису событий с пространствами имен ONVIF events.
func soapEvent(ctx context.Context, url string, creds Credentials, bodyAction string, digest bool) ([]byte, error) {
	header := ""
	if creds.Username != "" {
		header = "<s:Header>" + usernameToken(creds, digest) + "</s:Header>"
	}

	doc := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" ` +
		`xmlns:tev="http://www.onvif.org/ver10/events/wsdl" ` +
		`xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2" ` +
		`xmlns:tt="http://www.onvif.org/ver10/schema">` +
		header +
		`<s:Body>` + bodyAction + `</s:Body>` +
		`</s:Envelope>`

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte(doc)))
	if err != nil {
		return nil, fmt.Errorf("onvif events create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")

	res, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("onvif events request %s: %w", url, err)
	}
	defer res.Body.Close()

	data, _ := io.ReadAll(res.Body)

	if res.StatusCode >= 300 && res.StatusCode < 400 {
		return nil, fmt.Errorf("onvif events %s: status %d (redirect)", url, res.StatusCode)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("onvif events %s: status %d", url, res.StatusCode)
	}
	if bytes.Contains(data, []byte("Fault")) {
		return nil, fmt.Errorf("onvif events %s: soap fault: %s", url, soapFaultText(data))
	}
	return data, nil
}

// parseEvents извлекает уведомления из ответа PullMessages.
func parseEvents(data []byte) []EventMessage {
	decoder := xml.NewDecoder(bytes.NewReader(data))

	var msgs []EventMessage
	var cur *EventMessage
	topicCapture := false

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return msgs
		}

		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "NotificationMessage":
				cur = &EventMessage{Items: make(map[string]string)}
			case "Topic":
				if cur != nil {
					topicCapture = true
				}
			case "Message":
				if cur != nil {
					for _, a := range el.Attr {
						if a.Name.Local == "UtcTime" {
							if t, err := time.Parse(time.RFC3339, a.Value); err == nil {
								cur.Time = t
							}
						}
					}
				}
			case "SimpleItem":
				if cur != nil {
					var name, value string
					for _, a := range el.Attr {
						switch a.Name.Local {
						case "Name":
							name = a.Value
						case "Value":
							value = a.Value
						}
					}
					if name != "" {
						cur.Items[name] = value
					}
				}
			}
		case xml.CharData:
			if topicCapture && cur != nil {
				cur.Topic += string(el)
			}
		case xml.EndElement:
			switch el.Name.Local {
			case "Topic":
				topicCapture = false
			case "NotificationMessage":
				if cur != nil {
					msgs = append(msgs, *cur)
					cur = nil
				}
			}
		}
	}

	return msgs
}

// IsMotionTopic определяет, относится ли событие к детектору движения.
func (m EventMessage) IsMotionTopic() bool {
	t := strings.ToLower(m.Topic)
	return strings.Contains(t, "cellmotion") ||
		strings.Contains(t, "motiondetector") ||
		strings.Contains(t, "humandetection") ||
		strings.Contains(t, "motion")
}

// IsActive определяет, активирован ли детектор в сообщении.
func (m EventMessage) IsActive() bool {
	for _, v := range m.Items {
		switch strings.ToLower(v) {
		case "true", "1":
			return true
		}
	}
	// Сообщение без явных значений считаем началом срабатывания.
	return len(m.Items) == 0
}
