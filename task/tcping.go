package task

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/utils"
)

const (
	maxRoutine       = 1000
	defaultRoutines  = 200
	defaultPort      = 443
	defaultPingTimes = 4
)

var (
	Routines          = defaultRoutines
	TCPPort       int = defaultPort
	PingTimes     int = defaultPingTimes
	// 修复：TCP 超时现在可通过 -ct 参数配置，原版硬编码 1s
	TCPConnectTimeout = time.Second
)

type Ping struct {
	wg  sync.WaitGroup
	mu  sync.Mutex
	ctx context.Context
	ips []*net.IPAddr
	csv utils.PingDelaySet
	sem chan struct{}
	bar *utils.Bar
}

func checkPingDefaults() {
	if Routines <= 0 {
		Routines = defaultRoutines
	}
	if Routines > maxRoutine {
		Routines = maxRoutine
	}
	if TCPPort <= 0 || TCPPort >= 65535 {
		TCPPort = defaultPort
	}
	if PingTimes <= 0 {
		PingTimes = defaultPingTimes
	}
	if TCPConnectTimeout <= 0 {
		TCPConnectTimeout = time.Second
	}
}

func NewPing(ctx context.Context) *Ping {
	checkPingDefaults()
	ips := loadIPRanges()
	return &Ping{
		ctx: ctx,
		ips: ips,
		csv: make(utils.PingDelaySet, 0, len(ips)),
		sem: make(chan struct{}, Routines),
		bar: utils.NewBar(len(ips), "可用:", ""),
	}
}

func (p *Ping) Run() utils.PingDelaySet {
	if len(p.ips) == 0 {
		return p.csv
	}

	mode := "TCP"
	if Httping {
		mode = "HTTP"
	}
	utils.Cyan.Printf("开始延迟测速（模式：%s, 端口：%d, 范围：%d~%d ms, 丢包上限：%.2f）\n",
		mode, TCPPort,
		utils.InputMinDelay.Milliseconds(), utils.InputMaxDelay.Milliseconds(),
		utils.InputMaxLossRate,
	)

	for _, ip := range p.ips {
		select {
		case <-p.ctx.Done():
			utils.Yellow.Println("\n[中断] 收到停止信号，结束延迟测速...")
			goto done
		default:
		}
		p.wg.Add(1)
		p.sem <- struct{}{}
		go p.worker(ip)
	}
done:
	p.wg.Wait()
	p.bar.Done()
	sort.Sort(p.csv)
	return p.csv
}

func (p *Ping) worker(ip *net.IPAddr) {
	defer func() {
		p.wg.Done()
		<-p.sem
	}()
	p.tcpingHandler(ip)
}

func (p *Ping) tcping(ip *net.IPAddr) (bool, time.Duration) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", formatAddr(ip, TCPPort), TCPConnectTimeout)
	if err != nil {
		return false, 0
	}
	defer conn.Close()
	return true, time.Since(start)
}

func (p *Ping) checkConnection(ip *net.IPAddr) (recv int, total time.Duration, colo string) {
	if Httping {
		return p.httping(ip)
	}
	for i := 0; i < PingTimes; i++ {
		if ok, d := p.tcping(ip); ok {
			recv++
			total += d
		}
	}
	return
}

func (p *Ping) appendIPData(data *utils.PingData) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.csv = append(p.csv, utils.CloudflareIPData{PingData: data})
}

func (p *Ping) tcpingHandler(ip *net.IPAddr) {
	recv, totalDelay, colo := p.checkConnection(ip)

	available := len(p.csv)
	if recv != 0 {
		available++
	}
	p.bar.Grow(1, strconv.Itoa(available))

	if recv == 0 {
		return
	}
	p.appendIPData(&utils.PingData{
		IP:       ip,
		Sended:   PingTimes,
		Received: recv,
		Delay:    totalDelay / time.Duration(recv),
		Colo:     colo,
	})
}

// formatAddr 格式化 "IP:port"，IPv6 自动加方括号
func formatAddr(ip *net.IPAddr, port int) string {
	if isIPv4(ip.String()) {
		return fmt.Sprintf("%s:%d", ip.String(), port)
	}
	return fmt.Sprintf("[%s]:%d", ip.String(), port)
}
