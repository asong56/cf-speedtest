package task

import (
	"context"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/VividCortex/ewma"
	"github.com/XIU2/CloudflareSpeedTest/utils"
)

const (
	// 修复：原值 1024 字节太小，频繁系统调用影响速度，改为 8 KB
	bufferSize     = 8 * 1024
	defaultURL     = "https://cf.xiu2.xyz/url"
	defaultTimeout = 10 * time.Second
	defaultTestNum = 10
)

var (
	URL      = defaultURL
	Timeout  = defaultTimeout
	Disable  = false
	TestCount = defaultTestNum
	MinSpeed  = 0.0
)

func checkDownloadDefaults() {
	if URL == "" {
		URL = defaultURL
	}
	if Timeout <= 0 {
		Timeout = defaultTimeout
	}
	if TestCount <= 0 {
		TestCount = defaultTestNum
	}
}

// TestDownloadSpeed 对延迟测速结果逐一执行下载测速
func TestDownloadSpeed(ctx context.Context, ipSet utils.PingDelaySet) utils.DownloadSpeedSet {
	checkDownloadDefaults()

	if Disable {
		return utils.DownloadSpeedSet(ipSet)
	}
	if len(ipSet) == 0 {
		utils.Yellow.Println("[信息] 延迟测速 IP 数量为 0，跳过下载测速。")
		return nil
	}

	testNum := TestCount
	if len(ipSet) < TestCount || MinSpeed > 0 {
		testNum = len(ipSet)
	}
	if testNum < TestCount {
		TestCount = testNum
	}

	utils.Cyan.Printf("开始下载测速（下限：%.2f MB/s, 目标：%d 个, 队列：%d 个）\n",
		MinSpeed, TestCount, testNum)

	// 让进度条与延迟测速进度条对齐（强迫症）
	pad := strings.Repeat(" ", len(strconv.Itoa(len(ipSet))))
	bar := utils.NewBar(TestCount, "     "+pad, "")

	var speedSet utils.DownloadSpeedSet

	for i := 0; i < testNum; i++ {
		select {
		case <-ctx.Done():
			utils.Yellow.Println("\n[中断] 收到停止信号，结束下载测速...")
			goto done
		default:
		}

		speed, colo := downloadHandler(ctx, ipSet[i].IP)
		ipSet[i].DownloadSpeed = speed
		if ipSet[i].Colo == "" {
			ipSet[i].Colo = colo
		}

		if speed >= MinSpeed*1024*1024 {
			bar.Grow(1, "")
			speedSet = append(speedSet, ipSet[i])
			if len(speedSet) == TestCount {
				break
			}
		}
	}
done:
	bar.Done()

	if MinSpeed == 0 {
		speedSet = utils.DownloadSpeedSet(ipSet)
	} else if utils.Debug && len(speedSet) == 0 {
		utils.Yellow.Println("[调试] 无 IP 满足下载速度下限，返回全量数据以供参考。")
		speedSet = utils.DownloadSpeedSet(ipSet)
	}

	sort.Sort(speedSet)
	return speedSet
}

// getDialContext 返回强制路由到指定 IP 的 DialContext
func getDialContext(ip *net.IPAddr) func(ctx context.Context, network, address string) (net.Conn, error) {
	addr := formatAddr(ip, TCPPort)
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
}

// downloadHandler 对单个 IP 执行下载测速，返回 (bytes/sec, 地区码)
func downloadHandler(ctx context.Context, ip *net.IPAddr) (float64, string) {
	var lastRedirectURL string

	client := &http.Client{
		Transport: &http.Transport{DialContext: getDialContext(ip)},
		Timeout:   Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			lastRedirectURL = req.URL.String()
			if len(via) > 10 {
				if utils.Debug {
					utils.Red.Printf("[调试] %s 重定向次数超过 10 次，终止\n", ip)
				}
				return http.ErrUseLastResponse
			}
			if req.Header.Get("Referer") == defaultURL {
				req.Header.Del("Referer")
			}
			return nil
		},
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, URL, nil)
	if err != nil {
		if utils.Debug {
			utils.Red.Printf("[调试] %s 创建请求失败：%v\n", ip, err)
		}
		return 0, ""
	}
	req.Header.Set("User-Agent", defaultUA)

	resp, err := client.Do(req)
	if err != nil {
		if utils.Debug {
			logDownloadError(ip, err, 0, lastRedirectURL, nil)
		}
		return 0, ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if utils.Debug {
			logDownloadError(ip, nil, resp.StatusCode, lastRedirectURL, resp)
		}
		return 0, ""
	}

	colo := getHeaderColo(resp.Header)

	timeStart := time.Now()
	timeEnd := timeStart.Add(Timeout)
	timeSlice := Timeout / 100 // 将测速时段等分为 100 个时间片

	contentLength := resp.ContentLength
	buffer := make([]byte, bufferSize)

	var (
		contentRead     int64
		lastContentRead int64
		timeCounter     = 1
	)
	nextTime := timeStart.Add(timeSlice * time.Duration(timeCounter))
	e := ewma.NewMovingAverage()

	for contentLength != contentRead {
		now := time.Now()
		if now.After(nextTime) {
			timeCounter++
			nextTime = timeStart.Add(timeSlice * time.Duration(timeCounter))
			e.Add(float64(contentRead - lastContentRead))
			lastContentRead = contentRead
		}
		if now.After(timeEnd) {
			break
		}
		n, err := resp.Body.Read(buffer)
		contentRead += int64(n)
		if err != nil {
			if err == io.EOF {
				if contentLength == -1 {
					break
				}
				// 文件下载提前完成：补算最后一片的速度
				lastSlice := timeStart.Add(timeSlice * time.Duration(timeCounter-1))
				elapsed := float64(now.Sub(lastSlice)) / float64(timeSlice)
				if elapsed > 0 {
					e.Add(float64(contentRead-lastContentRead) / elapsed)
				}
			}
			break
		}
	}

	// 修复原版速度公式：
	// e.Value() 单位 = bytes / timeSlice，timeSlice = Timeout/100
	// bytes/sec = e.Value() / timeSlice.Seconds()
	//           = e.Value() / (Timeout.Seconds() / 100)
	// 原版错误地写成 / (Timeout.Seconds() / 120)，导致速度虚高约 20%
	return e.Value() / (Timeout.Seconds() / 100), colo
}

func logDownloadError(ip *net.IPAddr, err error, statusCode int, redirectURL string, resp *http.Response) {
	finalURL := URL
	if redirectURL != "" {
		finalURL = redirectURL
	} else if resp != nil && resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	redirected := finalURL != URL

	if statusCode > 0 {
		if redirected {
			utils.Red.Printf("[调试] %s 下载终止，状态码：%d，最终地址：%s\n", ip, statusCode, finalURL)
		} else {
			utils.Red.Printf("[调试] %s 下载终止，状态码：%d\n", ip, statusCode)
		}
	} else if err != nil {
		if redirected {
			utils.Red.Printf("[调试] %s 下载失败：%v，最终地址：%s\n", ip, err, finalURL)
		} else {
			utils.Red.Printf("[调试] %s 下载失败：%v\n", ip, err)
		}
	}
}
