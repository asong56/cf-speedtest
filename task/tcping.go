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
	maxRoutines      = 1000
	defaultRoutines  = 200
	defaultPort      = 443
	defaultPingTimes = 4
)

// ProbeMode selects which protocol the latency test uses.
type ProbeMode string

const (
	ModeTCP  ProbeMode = "tcp"
	ModeICMP ProbeMode = "icmp"
	ModeHTTP ProbeMode = "http"
)

func ParseMode(s string) ProbeMode {
	switch s {
	case "icmp":
		return ModeICMP
	case "http", "https", "httping":
		return ModeHTTP
	default:
		return ModeTCP
	}
}

var (
	Routines              = defaultRoutines
	TCPPort           int  = defaultPort
	PingTimes         int  = defaultPingTimes
	TCPConnectTimeout      = time.Second
	Mode                   = ModeTCP
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
	if Routines > maxRoutines {
		Routines = maxRoutines
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
		bar: utils.NewBar(len(ips), "Available:", ""),
	}
}

func (p *Ping) Run() utils.PingDelaySet {
	if len(p.ips) == 0 {
		return p.csv
	}

	utils.Cyan.Printf("Latency test started (mode: %s, port: %d, range: %d~%dms, max loss: %.2f)\n",
		Mode, TCPPort,
		utils.InputMinDelay.Milliseconds(), utils.InputMaxDelay.Milliseconds(),
		utils.InputMaxLossRate,
	)

	for _, ip := range p.ips {
		select {
		case <-p.ctx.Done():
			utils.Yellow.Println("\n[interrupt] stop signal received, ending latency test...")
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
	p.handle(ip)
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

// probe dispatches to the configured protocol and returns per-attempt RTTs plus an optional colo code.
func (p *Ping) probe(ip *net.IPAddr) ([]time.Duration, string) {
	switch Mode {
	case ModeHTTP:
		return p.httping(ip)
	case ModeICMP:
		return p.icmping(ip)
	default:
		var rtts []time.Duration
		for i := 0; i < PingTimes; i++ {
			if ok, d := p.tcping(ip); ok {
				rtts = append(rtts, d)
			}
		}
		return rtts, ""
	}
}

func (p *Ping) appendIPData(data *utils.PingData) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.csv = append(p.csv, utils.CloudflareIPData{PingData: data})
}

func (p *Ping) handle(ip *net.IPAddr) {
	rtts, colo := p.probe(ip)

	available := len(p.csv)
	if len(rtts) > 0 {
		available++
	}
	p.bar.Grow(1, strconv.Itoa(available))

	if len(rtts) == 0 {
		return
	}
	recv, avg, jitter := computeStats(rtts)
	p.appendIPData(&utils.PingData{
		IP:       ip,
		Sended:   PingTimes,
		Received: recv,
		Delay:    avg,
		Jitter:   jitter,
		Colo:     colo,
	})
}

// computeStats derives average delay and jitter (max-min swing) from raw RTT samples.
func computeStats(rtts []time.Duration) (received int, avg, jitter time.Duration) {
	received = len(rtts)
	if received == 0 {
		return
	}
	min, max, sum := rtts[0], rtts[0], time.Duration(0)
	for _, d := range rtts {
		sum += d
		if d < min {
			min = d
		}
		if d > max {
			max = d
		}
	}
	avg = sum / time.Duration(received)
	jitter = max - min
	return
}

func formatAddr(ip *net.IPAddr, port int) string {
	if isIPv4(ip.String()) {
		return fmt.Sprintf("%s:%d", ip.String(), port)
	}
	return fmt.Sprintf("[%s]:%d", ip.String(), port)
}
