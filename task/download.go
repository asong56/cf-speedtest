package task

import (
	"context"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/utils"
)

const (
	bufferSize     = 32 * 1024
	defaultURL     = "https://cf.xiu2.xyz/url"
	defaultTimeout = 10 * time.Second
	defaultTestNum = 10
)

var (
	URL             = defaultURL
	Timeout         = defaultTimeout
	Disable         = false
	TestCount       = defaultTestNum
	MinSpeed        = 0.0
	DownloadThreads = 1
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
	if DownloadThreads <= 0 {
		DownloadThreads = 1
	}
}

// TestDownloadSpeed runs the real HTTP download test over the latency-filtered candidates.
func TestDownloadSpeed(ctx context.Context, ipSet utils.PingDelaySet) utils.DownloadSpeedSet {
	checkDownloadDefaults()

	if Disable {
		return utils.DownloadSpeedSet(ipSet)
	}
	if len(ipSet) == 0 {
		utils.Yellow.Println("[info] latency test returned 0 IPs, skipping download test.")
		return nil
	}

	testNum := TestCount
	if len(ipSet) < TestCount || MinSpeed > 0 {
		testNum = len(ipSet)
	}
	if testNum < TestCount {
		TestCount = testNum
	}

	utils.Cyan.Printf("Download test started (min speed: %.2f MB/s, target: %d, pool: %d, threads/IP: %d)\n",
		MinSpeed, TestCount, testNum, DownloadThreads)

	pad := strings.Repeat(" ", len(strconv.Itoa(len(ipSet))))
	bar := utils.NewBar(TestCount, "     "+pad, "")

	var speedSet utils.DownloadSpeedSet

	for i := 0; i < testNum; i++ {
		select {
		case <-ctx.Done():
			utils.Yellow.Println("\n[interrupt] stop signal received, ending download test...")
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
		utils.Yellow.Println("[debug] no IP met the minimum speed, returning full set for reference.")
		speedSet = utils.DownloadSpeedSet(ipSet)
	}

	sort.Sort(speedSet)
	return speedSet
}

func getDialContext(ip *net.IPAddr) func(ctx context.Context, network, address string) (net.Conn, error) {
	addr := formatAddr(ip, TCPPort)
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
}

// downloadHandler fans out to DownloadThreads parallel connections per IP and sums their throughput.
func downloadHandler(ctx context.Context, ip *net.IPAddr) (float64, string) {
	if DownloadThreads <= 1 {
		return downloadOnce(ctx, ip)
	}
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		total float64
		colo  string
	)
	wg.Add(DownloadThreads)
	for i := 0; i < DownloadThreads; i++ {
		go func() {
			defer wg.Done()
			speed, c := downloadOnce(ctx, ip)
			mu.Lock()
			total += speed
			if colo == "" && c != "" {
				colo = c
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return total, colo
}

// downloadOnce measures average throughput of a single connection over the configured Timeout window.
func downloadOnce(ctx context.Context, ip *net.IPAddr) (float64, string) {
	client := &http.Client{
		Transport: &http.Transport{DialContext: getDialContext(ip)},
		Timeout:   Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 10 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, URL, nil)
	if err != nil {
		return 0, ""
	}
	req.Header.Set("User-Agent", defaultUA)

	resp, err := client.Do(req)
	if err != nil {
		if utils.Debug {
			utils.Red.Printf("[debug] %s download failed: %v\n", ip, err)
		}
		return 0, ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if utils.Debug {
			utils.Red.Printf("[debug] %s download aborted, status %d\n", ip, resp.StatusCode)
		}
		return 0, ""
	}

	colo := getHeaderColo(resp.Header)

	buffer := make([]byte, bufferSize)
	start := time.Now()
	deadline := start.Add(Timeout)
	var read int64

	for time.Now().Before(deadline) {
		n, err := resp.Body.Read(buffer)
		read += int64(n)
		if err != nil {
			break
		}
	}

	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		return 0, colo
	}
	return float64(read) / elapsed, colo
}
