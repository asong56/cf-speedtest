package task

import (
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/utils"
)

var (
	HttpingStatusCode int
	HttpingCFColo     string
	HttpingCFColomap  *sync.Map

	reIATA    = regexp.MustCompile(`[A-Z]{3}`)
	reCountry = regexp.MustCompile(`[A-Z]{2}`)
	reGcore   = regexp.MustCompile(`^[a-z]{2,4}`)
)

const defaultUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

func (p *Ping) httping(ip *net.IPAddr) ([]time.Duration, string) {
	hc := http.Client{
		Timeout:   time.Second * 2,
		Transport: &http.Transport{DialContext: getDialContext(ip)},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer hc.CloseIdleConnections()

	colo, ok := p.httpingPrecheck(&hc, ip)
	if !ok {
		return nil, ""
	}

	var rtts []time.Duration
	for i := 0; i < PingTimes; i++ {
		req, err := http.NewRequest(http.MethodHead, URL, nil)
		if err != nil {
			break
		}
		req.Header.Set("User-Agent", defaultUA)
		if i == PingTimes-1 {
			req.Header.Set("Connection", "close")
		}
		start := time.Now()
		resp, err := hc.Do(req)
		if err != nil {
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		rtts = append(rtts, time.Since(start))
	}
	return rtts, colo
}

func (p *Ping) httpingPrecheck(hc *http.Client, ip *net.IPAddr) (string, bool) {
	req, err := http.NewRequest(http.MethodHead, URL, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("User-Agent", defaultUA)

	resp, err := hc.Do(req)
	if err != nil {
		if utils.Debug {
			utils.Red.Printf("[debug] %s request failed: %v\n", ip, err)
		}
		return "", false
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	// valid range is 100-599; outside it we fall back to the 200/301/302 default
	if HttpingStatusCode == 0 || HttpingStatusCode < 100 || HttpingStatusCode > 599 {
		code := resp.StatusCode
		if code != 200 && code != 301 && code != 302 {
			if utils.Debug {
				utils.Red.Printf("[debug] %s status %d not in default allowed set (200/301/302)\n", ip, code)
			}
			return "", false
		}
	} else if resp.StatusCode != HttpingStatusCode {
		if utils.Debug {
			utils.Red.Printf("[debug] %s status %d != expected %d\n", ip, resp.StatusCode, HttpingStatusCode)
		}
		return "", false
	}

	colo := getHeaderColo(resp.Header)
	if HttpingCFColo != "" {
		colo = p.filterColo(colo)
		if colo == "" {
			if utils.Debug {
				utils.Red.Printf("[debug] %s colo not in requested set\n", ip)
			}
			return "", false
		}
	}
	return colo, true
}

func MapColoMap() *sync.Map {
	if HttpingCFColo == "" {
		return nil
	}
	sm := &sync.Map{}
	for _, c := range strings.Split(strings.ToUpper(HttpingCFColo), ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			sm.Store(c, struct{}{})
		}
	}
	return sm
}

// getHeaderColo extracts a colo/IATA code from common CDN response headers
// (Cloudflare, CDN77, BunnyCDN, CloudFront, Fastly, Gcore).
func getHeaderColo(h http.Header) string {
	switch h.Get("server") {
	case "cloudflare":
		if ray := h.Get("cf-ray"); ray != "" {
			return reIATA.FindString(ray)
		}
	case "CDN77-Turbo":
		if pop := h.Get("x-77-pop"); pop != "" {
			return reCountry.FindString(pop)
		}
	default:
		if srv := h.Get("server"); strings.HasPrefix(srv, "BunnyCDN-") {
			return reCountry.FindString(strings.TrimPrefix(srv, "BunnyCDN-"))
		}
	}
	if pop := h.Get("x-amz-cf-pop"); pop != "" {
		return reIATA.FindString(pop)
	}
	if srvBy := h.Get("x-served-by"); srvBy != "" {
		if m := reIATA.FindAllString(srvBy, -1); len(m) > 0 {
			return m[len(m)-1]
		}
	}
	if fe := h.Get("x-id-fe"); fe != "" {
		if c := reGcore.FindString(fe); c != "" {
			return strings.ToUpper(c)
		}
	}
	return ""
}

func (p *Ping) filterColo(colo string) string {
	if colo == "" || HttpingCFColomap == nil {
		return colo
	}
	if _, ok := HttpingCFColomap.Load(colo); ok {
		return colo
	}
	return ""
}
