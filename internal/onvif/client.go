package onvif

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Credentials — учётные данные ONVIF-устройства.
type Credentials struct {
	Username string
	Password string
}

// Profile — профиль медиа-сервиса камеры.
type Profile struct {
	Token string
	Name  string
}

const (
	// requestTimeout — таймаут одной SOAP-попытки.
	requestTimeout = 5 * time.Second
	// callDeadline — суммарный дедлайн одной ONVIF-операции.
	callDeadline = 20 * time.Second
)

// httpClient не следует редиректам: веб-серверы камер часто отвечают 302
// на неизвестные пути, перенаправляя на HTML-страницу входа.
var httpClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// mediaPaths — типовые пути медиа-сервиса ONVIF у разных производителей.
var mediaPaths = []string{
	"/onvif/media_service",
	"/onvif/media",
	"/media_service",
	"/onvif/media_svc",
}

func mediaURLs(host string, port int) []string {
	urls := make([]string, 0, len(mediaPaths))
	for _, p := range mediaPaths {
		urls = append(urls, fmt.Sprintf("http://%s:%d%s", host, port, p))
	}
	return urls
}

// GetProfiles запрашивает список профилей медиа-сервиса камеры.
func GetProfiles(ctx context.Context, host string, port int, creds Credentials) ([]Profile, error) {
	resp, err := callMedia(ctx, host, port, creds, `<trt:GetProfiles/>`)
	if err != nil {
		return nil, err
	}
	return parseProfiles(resp)
}

// GetStreamUri запрашивает RTSP-адрес потока для указанного профиля.
// Минималистичные прошивки отклоняют каноническое тело запроса, поэтому
// перебираются несколько вариантов тела в рамках общего дедлайна.
func GetStreamUri(ctx context.Context, host string, port int, creds Credentials, profileToken string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callDeadline)
	defer cancel()

	if profileToken == "" {
		profiles, err := GetProfiles(ctx, host, port, creds)
		if err != nil {
			return "", err
		}
		if len(profiles) == 0 {
			return "", fmt.Errorf("onvif: no media profiles found")
		}
		profileToken = profiles[0].Token
	}

	var lastErr error
	for _, body := range getStreamUriBodies(profileToken) {
		resp, err := callMedia(ctx, host, port, creds, body)
		if err == nil {
			uri := firstElementText(resp, "Uri")
			if uri != "" {
				return uri, nil
			}
			lastErr = fmt.Errorf("onvif: stream uri not found in response")
			continue
		}

		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	return "", lastErr
}

// getStreamUriBodies возвращает варианты тела GetStreamUri:
// канонический порядок, минимальный и обратный порядок элементов.
func getStreamUriBodies(profileToken string) []string {
	esc := xmlEscape(profileToken)
	streamSetup := "<trt:StreamSetup><tt:Stream>RTP_UNICAST</tt:Stream>" +
		"<tt:Transport><tt:Protocol>RTSP</tt:Protocol></tt:Transport></trt:StreamSetup>"

	return []string{
		"<trt:GetStreamUri>" + streamSetup +
			"<trt:ProfileToken>" + esc + "</trt:ProfileToken></trt:GetStreamUri>",

		"<trt:GetStreamUri><trt:ProfileToken>" + esc +
			"</trt:ProfileToken></trt:GetStreamUri>",

		"<trt:GetStreamUri><trt:ProfileToken>" + esc + "</trt:ProfileToken>" +
			streamSetup + "</trt:GetStreamUri>",
	}
}

// callMedia обращается к медиа-сервису, перебирая типовые пути,
// в рамках общего дедлайна callDeadline.
func callMedia(ctx context.Context, host string, port int, creds Credentials, bodyAction string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, callDeadline)
	defer cancel()

	var lastErr error
	for _, u := range mediaURLs(host, port) {
		data, err := callURL(ctx, u, creds, bodyAction)
		if err == nil {
			return data, nil
		}
		lastErr = err

		if ctx.Err() != nil {
			break
		}
	}
	return nil, lastErr
}

// callURL сначала пробует PasswordDigest, затем фолбэк на PasswordText.
func callURL(ctx context.Context, url string, creds Credentials, bodyAction string) ([]byte, error) {
	data, err := doSOAP(ctx, url, creds, bodyAction, true)
	if err == nil {
		return data, nil
	}
	digestErr := err

	data, err = doSOAP(ctx, url, creds, bodyAction, false)
	if err == nil {
		return data, nil
	}
	return nil, digestErr
}

func doSOAP(ctx context.Context, url string, creds Credentials, bodyAction string, digest bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	envelope := buildEnvelope(creds, bodyAction, digest)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(envelope))
	if err != nil {
		return nil, fmt.Errorf("onvif create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")

	res, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("onvif request %s: %w", url, err)
	}
	defer res.Body.Close()

	data, _ := io.ReadAll(res.Body)

	// Редирект означает, что на этом порту/пути сидит веб-интерфейс,
	// а не ONVIF-сервис.
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		return nil, fmt.Errorf("onvif %s: status %d (redirect: not an ONVIF service on this port/path)", url, res.StatusCode)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("onvif %s: status %d", url, res.StatusCode)
	}
	if bytes.Contains(data, []byte("Fault")) {
		return nil, fmt.Errorf("onvif %s: soap fault: %s", url, soapFaultText(data))
	}
	return data, nil
}

func buildEnvelope(creds Credentials, bodyAction string, digest bool) []byte {
	header := ""
	if creds.Username != "" {
		header = "<s:Header>" + usernameToken(creds, digest) + "</s:Header>"
	}

	doc := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" ` +
		`xmlns:trt="http://www.onvif.org/ver10/media/wsdl" ` +
		`xmlns:tt="http://www.onvif.org/ver10/schema">` +
		header +
		`<s:Body>` + bodyAction + `</s:Body>` +
		`</s:Envelope>`

	return []byte(doc)
}

// usernameToken формирует WS-UsernameToken по стандарту ONVIF.
func usernameToken(creds Credentials, digest bool) string {
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	created := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	var passwordElement string
	if digest {
		h := sha1.New()
		h.Write(nonce)
		h.Write([]byte(created))
		h.Write([]byte(creds.Password))
		passwordElement = fmt.Sprintf(
			`<Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">%s</Password>`,
			base64.StdEncoding.EncodeToString(h.Sum(nil)),
		)
	} else {
		passwordElement = fmt.Sprintf(
			`<Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordText">%s</Password>`,
			xmlEscape(creds.Password),
		)
	}

	return fmt.Sprintf(
		`<Security s:mustUnderstand="0" xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd">`+
			`<UsernameToken>`+
			`<Username>%s</Username>%s`+
			`<Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">%s</Nonce>`+
			`<Created xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">%s</Created>`+
			`</UsernameToken></Security>`,
		xmlEscape(creds.Username),
		passwordElement,
		base64.StdEncoding.EncodeToString(nonce),
		created,
	)
}

// parseProfiles устойчиво извлекает профили из ответа GetProfiles.
func parseProfiles(data []byte) ([]Profile, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))

	var profiles []Profile
	var current *Profile
	capturingName := false

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("onvif parse profiles: %w", err)
		}

		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "Profiles":
				token := ""
				for _, a := range el.Attr {
					if a.Name.Local == "token" {
						token = a.Value
					}
				}
				current = &Profile{Token: token}
			case "Name":
				if current != nil {
					capturingName = true
				}
			}
		case xml.CharData:
			if capturingName && current != nil {
				current.Name += string(el)
			}
		case xml.EndElement:
			switch el.Name.Local {
			case "Profiles":
				if current != nil {
					profiles = append(profiles, *current)
					current = nil
				}
			case "Name":
				capturingName = false
			}
		}
	}

	return profiles, nil
}

// firstElementText возвращает текст первого элемента с указанным локальным именем.
func firstElementText(data []byte, localName string) string {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	capture := false
	var sb strings.Builder

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ""
		}

		switch el := tok.(type) {
		case xml.StartElement:
			if el.Name.Local == localName {
				capture = true
				sb.Reset()
			}
		case xml.CharData:
			if capture {
				sb.Write(el)
			}
		case xml.EndElement:
			if capture && el.Name.Local == localName {
				return strings.TrimSpace(sb.String())
			}
		}
	}

	return strings.TrimSpace(sb.String())
}

func soapFaultText(data []byte) string {
	if t := firstElementText(data, "Text"); t != "" {
		return t
	}
	s := string(data)
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// xmlEscape экранирует спецсимволы XML в строке.
func xmlEscape(s string) string {
	var sb strings.Builder
	_ = xml.EscapeText(&sb, []byte(s))
	return sb.String()
}