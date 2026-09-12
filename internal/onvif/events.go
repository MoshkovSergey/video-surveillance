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

const (
	subscribeTimeout = 8 * time.Second
	pullTimeout      = 20 * time.Second
	pullWait         = 5 * time.Second
)

// EventMessage — сообщение события ONVIF.
type EventMessage struct {
	Topic  string
	Active bool
}

// IsMotionTopic распознаёт темы детектора движения.
func (m EventMessage) IsMotionTopic() bool {
	t := strings.ToLower(m.Topic)
	return strings.Contains(t, "motion") ||
		strings.Contains(t, "cellmotion") ||
		strings.Contains(t, "motionalarm")
}

// IsActive возвращает true, если движение активно.
func (m EventMessage) IsActive() bool { return m.Active }

func eventsURLs(host string, port int) []string {
	return []string{
		fmt.Sprintf("http://%s:%d/onvif/event_service", host, port),
		fmt.Sprintf("http://%s:%d/onvif/events", host, port),
	}
}

func eventsEnvelope(creds Credentials, bodyAction string, digest bool) []byte {
	header := ""
	if creds.Username != "" {
		header = "<s:Header>" + usernameToken(creds, digest) + "</s:Header>"
	}

	doc := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" ` +
		`xmlns:tev="http://www.onvif.org/ver10/events/wsdl" ` +
		`xmlns:tt="http://www.onvif.org/ver10/schema">` +
		header +
		`<s:Body>` + bodyAction + `</s:Body>` +
		`</s:Envelope>`

	return []byte(doc)
}

func doEventsSOAP(ctx context.Context, url string, creds Credentials, bodyAction string, digest bool) ([]byte, error) {
	envelope := eventsEnvelope(creds, bodyAction, digest)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(envelope))
	if err != nil {
		return nil, fmt.Errorf("onvif events: создание запроса: %w", err)
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")

	res, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("onvif events: запрос %s: %w", url, err)
	}
	defer res.Body.Close()

	data, _ := io.ReadAll(res.Body)

	if res.StatusCode >= 300 && res.StatusCode < 400 {
		return nil, fmt.Errorf("onvif events %s: статус %d (редирект)", url, res.StatusCode)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("onvif events %s: статус %d", url, res.StatusCode)
	}
	if bytes.Contains(data, []byte("Fault")) {
		return nil, fmt.Errorf("onvif events %s: ошибка SOAP: %s", url, soapFaultText(data))
	}
	return data, nil
}

func callEvents(ctx context.Context, url string, creds Credentials, bodyAction string) ([]byte, error) {
	data, err := doEventsSOAP(ctx, url, creds, bodyAction, true)
	if err == nil {
		return data, nil
	}
	digestErr := err

	data, err = doEventsSOAP(ctx, url, creds, bodyAction, false)
	if err == nil {
		return data, nil
	}
	return nil, digestErr
}

// Subscribe создает PullPoint-подписку на события камеры.
func Subscribe(ctx context.Context, host string, port int, creds Credentials) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, subscribeTimeout)
	defer cancel()

	body := `<tev:CreatePullPointSubscription>` +
		`<tev:InitialTerminationTime>PT5M</tev:InitialTerminationTime>` +
		`</tev:CreatePullPointSubscription>`

	var lastErr error
	for _, u := range eventsURLs(host, port) {
		data, err := callEvents(ctx, u, creds, body)
		if err != nil {
			lastErr = err
			continue
		}

		addr := firstElementText(data, "Address")
		if addr == "" {
			lastErr = fmt.Errorf("onvif events %s: адрес подписки не найден в ответе", u)
			continue
		}
		return addr, nil
	}
	return "", lastErr
}

// Unsubscribe корректно закрывает подписку; ошибки игнорируются.
func Unsubscribe(ctx context.Context, subURL string, creds Credentials) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, _ = callEvents(ctx, subURL, creds, `<tev:Unsubscribe/>`)
	_, _ = callEvents(ctx, subURL, creds,
		`<wsnt:Unsubscribe xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2"/>`)
}

// PullMessages читает очередь событий подписки.
func PullMessages(ctx context.Context, subURL string, creds Credentials) ([]EventMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, pullTimeout)
	defer cancel()

	body := fmt.Sprintf(
		`<tev:PullMessages><tev:Timeout>PT%dS</tev:Timeout><tev:MessageLimit>64</tev:MessageLimit></tev:PullMessages>`,
		int(pullWait.Seconds()),
	)

	data, err := callEvents(ctx, subURL, creds, body)
	if err != nil {
		return nil, err
	}
	return parseMessages(data), nil
}

// parseMessages извлекает сообщения событий из ответа PullMessages.
func parseMessages(data []byte) []EventMessage {
	decoder := xml.NewDecoder(bytes.NewReader(data))

	var msgs []EventMessage
	var cur *EventMessage
	captureTopic := false

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
				m := EventMessage{}
				cur = &m
			case "Topic":
				if cur != nil {
					captureTopic = true
				}
			case "SimpleItem":
				if cur != nil {
					name, value := "", ""
					for _, a := range el.Attr {
						switch a.Name.Local {
						case "Name":
							name = a.Value
						case "Value":
							value = a.Value
						}
					}
					if strings.EqualFold(name, "IsMotion") ||
						strings.EqualFold(name, "State") ||
						strings.EqualFold(name, "MotionState") ||
						strings.EqualFold(name, "Active") {
						if value == "true" || value == "1" {
							cur.Active = true
						}
					}
				}
			}
		case xml.CharData:
			if captureTopic && cur != nil {
				cur.Topic += string(el)
			}
		case xml.EndElement:
			switch el.Name.Local {
			case "Topic":
				captureTopic = false
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
