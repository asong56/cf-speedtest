package task

import (
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/utils"
)

var (
	Httping           bool
	HttpingStatusCode int
	HttpingCFColo     string
	HttpingCFColomap  *sync.Map

	reIATA    = regexp.MustCompile(`[A-Z]{3}`)
	reCountry = regexp.MustCompile(`[A-Z]{2}`)
	reGcore   = regexp.MustCompile(`^[a-z]{2,4}`)
)

func (p *Ping) httping(ip *net.IPAddr) (int, time.Duration, string) {
	hc := http.Client{
		Timeout: time.Second * 2,
		Transport: &http.Transport{
			DialContext: getDialContext(ip),
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer hc.CloseIdleConnections()

	// 预检：校验状态码 + 获取地区码
	colo, ok := p.httpingPrecheck(&hc, ip)
	if !ok {
		return 0, 0, ""
	}

	// 正式延迟测量
	var (
		success int
		delay   time.Duration
	)
	for i := 0; i < PingTimes; i++ {
		req, err := http.NewRequest(http.MethodHead, URL, nil)
		if err != nil {
			log.Fatalf("创建 HTTP 请求失败：%v", err)
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
		success++
		delay += time.Since(start)
	}
	return success, delay, colo
}

const defaultUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

func (p *Ping) httpingPrecheck(hc *http.Client, ip *net.IPAddr) (string, bool) {
	req, err := http.NewRequest(http.MethodHead, URL, nil)
	if err != nil {
		if utils.Debug {
			utils.Red.Printf("[调试] %s 创建请求失败：%v\n", ip, err)
		}
		return "", false
	}
	req.Header.Set("User-Agent", defaultUA)

	resp, err := hc.Do(req)
	if err != nil {
		if utils.Debug {
			utils.Red.Printf("[调试] %s 请求失败：%v\n", ip, err)
		}
		return "", false
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	// 修复原版 Bug：
	// 原条件：HttpingStatusCode < 100 && HttpingStatusCode > 599
	// 两个条件不可能同时成立（AND），导致自定义状态码校验完全失效。
	// 正确条件：< 100 || > 599 （OR：超出合法范围则视为无效）
	if HttpingStatusCode == 0 || HttpingStatusCode < 100 || HttpingStatusCode > 599 {
		code := resp.StatusCode
		if code != 200 && code != 301 && code != 302 {
			if utils.Debug {
				utils.Red.Printf("[调试] %s 状态码 %d 不在默认允许范围（200/301/302）\n", ip, code)
			}
			return "", false
		}
	} else if resp.StatusCode != HttpingStatusCode {
		if utils.Debug {
			utils.Red.Printf("[调试] %s 状态码 %d ≠ 指定值 %d\n", ip, resp.StatusCode, HttpingStatusCode)
		}
		return "", false
	}

	colo := getHeaderColo(resp.Header)

	if HttpingCFColo != "" {
		colo = p.filterColo(colo)
		if colo == "" {
			if utils.Debug {
				utils.Red.Printf("[调试] %s 地区码不匹配指定范围\n", ip)
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

// getHeaderColo 从响应头解析地区码，支持 Cloudflare / CDN77 / BunnyCDN / CloudFront / Fastly / Gcore
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
