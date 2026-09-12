package onvif

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SystemTime — ответ GetSystemDateAndTime: UTC-время камеры и её пояс.
type SystemTime struct {
	UTC time.Time
	TZ  string
}

// GetSystemDateAndTime читает системное время камеры через ONVIF.
func GetSystemDateAndTime(ctx context.Context, host string, port int, creds Credentials) (SystemTime, error) {
	var st SystemTime

	urlStr := deviceURL(host, port)
	body := `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
<s:Body><tds:GetSystemDateAndTime/></s:Body></s:Envelope>`

	resp, err := postSOAP(ctx, urlStr, creds.Username, creds.Password, body)
	if err != nil {
		return st, err
	}
	if soapFault(resp) {
		return st, fmt.Errorf("soap fault: %s", firstLine(resp))
	}

	get := func(tag string) int {
		re := regexp.MustCompile(`<tt:` + tag + `>(\d+)</tt:` + tag + `>`)
		m := re.FindStringSubmatch(resp)
		if m == nil {
			return 0
		}
		v, _ := strconv.Atoi(m[1])
		return v
	}

	st.UTC = time.Date(
		get("Year"), time.Month(get("Month")), get("Day"),
		get("Hour"), get("Minute"), get("Second"), 0, time.UTC,
	)

	if m := regexp.MustCompile(`<tt:TZ>([^<]*)</tt:TZ>`).FindStringSubmatch(resp); m != nil {
		st.TZ = strings.TrimSpace(m[1])
	}
	return st, nil
}

// SetSystemDateAndTimeWithTZ устанавливает время камеры (UTC) и часовой пояс.
// Пустой tzString означает «не передавать элемент TimeZone».
func SetSystemDateAndTimeWithTZ(ctx context.Context, host string, port int, creds Credentials, now time.Time, tzString string) error {
	urlStr := deviceURL(host, port)

	utc := now.UTC()
	tz := ""
	if tzString != "" {
		tz = "<tds:TimeZone><tt:TZ>" + tzString + "</tt:TZ></tds:TimeZone>"
	}

	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
<s:Body><tds:SetSystemDateAndTime>
<tds:DateTimeType>Manual</tds:DateTimeType>
<tds:DaylightSavings>false</tds:DaylightSavings>
%s
<tds:UTCDateTime><tt:Time><tt:Hour>%d</tt:Hour><tt:Minute>%d</tt:Minute><tt:Second>%d</tt:Second></tt:Time><tt:Date><tt:Year>%d</tt:Year><tt:Month>%d</tt:Month><tt:Day>%d</tt:Day></tt:Date></tds:UTCDateTime>
</tds:SetSystemDateAndTime></s:Body></s:Envelope>`,
		tz, utc.Hour(), utc.Minute(), utc.Second(), utc.Year(), int(utc.Month()), utc.Day())

	resp, err := postSOAP(ctx, urlStr, creds.Username, creds.Password, body)
	if err != nil {
		return err
	}
	if soapFault(resp) {
		return fmt.Errorf("soap fault: %s", firstLine(resp))
	}
	return nil
}

func deviceURL(host string, port int) string {
	if port == 0 {
		return fmt.Sprintf("http://%s/onvif/device_service", host)
	}
	return fmt.Sprintf("http://%s:%d/onvif/device_service", host, port)
}

func soapFault(body string) bool {
	return strings.Contains(body, ":Fault>") || strings.Contains(body, "<Fault ")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// postSOAP выполняет SOAP POST с прозрачной Digest- или Basic-авторизацией.
func postSOAP(ctx context.Context, urlStr, user, pass, body string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	do := func(auth string) (int, string, string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, strings.NewReader(body))
		if err != nil {
			return 0, "", "", err
		}
		req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		res, err := client.Do(req)
		if err != nil {
			return 0, "", "", err
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header.Get("WWW-Authenticate"), string(b), nil
	}

	status, challenge, resp, err := do("")
	if err != nil {
		return "", err
	}
	if status != http.StatusUnauthorized {
		if status >= 400 {
			return "", fmt.Errorf("onvif http status %d: %s", status, firstLine(resp))
		}
		return resp, nil
	}

	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
	attempts := []string{basic}
	if strings.HasPrefix(strings.ToLower(challenge), "digest") {
		if h, derr := digestHeader(challenge, user, pass, http.MethodPost, urlStr); derr == nil {
			attempts = []string{h, basic}
		}
	}

	for _, auth := range attempts {
		status, _, resp, err = do(auth)
		if err != nil {
			return "", err
		}
		if status == http.StatusUnauthorized {
			continue
		}
		if status >= 400 {
			return "", fmt.Errorf("onvif http status %d: %s", status, firstLine(resp))
		}
		return resp, nil
	}
	return "", fmt.Errorf("onvif auth failed (401)")
}

// digestHeader вычисляет заголовок Authorization по RFC 7616 (MD5, qop=auth).
func digestHeader(challenge, user, pass, method, urlStr string) (string, error) {
	parts := map[string]string{}
	for _, f := range strings.Split(strings.TrimPrefix(challenge, "Digest "), ",") {
		kv := strings.SplitN(strings.TrimSpace(f), "=", 2)
		if len(kv) == 2 {
			parts[kv[0]] = strings.Trim(kv[1], `"`)
		}
	}
	realm, nonce, qop := parts["realm"], parts["nonce"], parts["qop"]
	if nonce == "" {
		return "", fmt.Errorf("bad digest challenge")
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}
	uri := u.RequestURI()

	h := func(s string) string {
		sum := md5.Sum([]byte(s))
		return hex.EncodeToString(sum[:])
	}
	ha1 := h(user + ":" + realm + ":" + pass)
	ha2 := h(method + ":" + uri)

	if strings.Contains(qop, "auth") {
		nc := "00000001"
		cnonce := h(fmt.Sprintf("%d", time.Now().UnixNano()))[:16]
		response := h(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":auth:" + ha2)
		return fmt.Sprintf(
			`Digest username="%s", realm="%s", nonce="%s", uri="%s", qop=auth, nc=%s, cnonce="%s", response="%s"`,
			user, realm, nonce, uri, nc, cnonce, response,
		), nil
	}

	response := h(ha1 + ":" + nonce + ":" + ha2)
	return fmt.Sprintf(
		`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`,
		user, realm, nonce, uri, response,
	), nil
}