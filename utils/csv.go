package utils

import (
	"encoding/csv"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
)

const (
	defaultOutput       = "result.csv"
	maxDelay            = 9999 * time.Millisecond
	minDelay            = 0 * time.Millisecond
	maxLossRate float32 = 1.0
	maxJitter           = 9999 * time.Millisecond
)

var (
	InputMaxDelay    = maxDelay
	InputMinDelay    = minDelay
	InputMaxLossRate = maxLossRate
	InputMaxJitter   = maxJitter
	Output           = defaultOutput
	PrintNum         = 10
	Debug            = false
	// Quiet suppresses status/progress chatter (banners, hints, progress bar).
	// The result table (unless -p 0) and the CSV file are still produced, since
	// those are the command's actual output, not information about the run.
	Quiet = false
)

// SortMode selects how the final result set is ordered.
type SortMode string

const (
	SortSpeed SortMode = "speed"
	SortDelay SortMode = "delay"
	SortScore SortMode = "score"
)

var ResultSort = SortSpeed

func NoPrintResult() bool { return PrintNum == 0 }
func noOutput() bool      { return Output == "" || Output == " " }

// PingData holds one IP's raw latency-test result.
type PingData struct {
	IP       *net.IPAddr
	Sended   int
	Received int
	Delay    time.Duration
	Jitter   time.Duration
	Colo     string
}

// CloudflareIPData adds download speed and geolocation to a PingData.
type CloudflareIPData struct {
	*PingData
	lossRate      float32
	DownloadSpeed float64
	ASN           string
}

func (cf *CloudflareIPData) getLossRate() float32 {
	if cf.lossRate == 0 && cf.Sended > 0 {
		cf.lossRate = float32(cf.Sended-cf.Received) / float32(cf.Sended)
	}
	return cf.lossRate
}

// score blends speed/delay/jitter/loss into one number for -sort score.
func (cf *CloudflareIPData) score() float64 {
	speed := cf.DownloadSpeed / 1024 / 1024
	delayMs := cf.Delay.Seconds() * 1000
	jitterMs := cf.Jitter.Seconds() * 1000
	return speed - delayMs/100 - jitterMs/100 - float64(cf.getLossRate())*20
}

func (cf *CloudflareIPData) toRow() []string {
	colo := cf.Colo
	if colo == "" {
		colo = "N/A"
	}
	asn := cf.ASN
	if asn == "" {
		asn = "N/A"
	}
	return []string{
		cf.IP.String(),
		strconv.Itoa(cf.Sended),
		strconv.Itoa(cf.Received),
		strconv.FormatFloat(float64(cf.getLossRate()), 'f', 2, 32),
		strconv.FormatFloat(cf.Delay.Seconds()*1000, 'f', 2, 32),
		strconv.FormatFloat(cf.Jitter.Seconds()*1000, 'f', 2, 32),
		strconv.FormatFloat(cf.DownloadSpeed/1024/1024, 'f', 2, 32),
		colo,
		asn,
	}
}

// ExportCsv writes results with a UTF-8 BOM so Excel opens them cleanly on Windows.
func ExportCsv(data []CloudflareIPData) {
	if noOutput() || len(data) == 0 {
		return
	}
	fp, err := os.Create(Output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] cannot create file %s: %v\n", Output, err)
		os.Exit(1)
	}
	defer fp.Close()

	fp.WriteString("\xEF\xBB\xBF")

	w := csv.NewWriter(fp)
	_ = w.Write([]string{"IP Address", "Sent", "Received", "Loss Rate", "Avg Delay(ms)", "Jitter(ms)", "Download Speed(MB/s)", "Colo", "ASN"})
	for _, v := range data {
		_ = w.Write(v.toRow())
	}
	w.Flush()
	if err := w.Error(); err != nil {
		fmt.Fprintf(os.Stderr, "[error] failed writing CSV: %v\n", err)
		os.Exit(1)
	}
}

// PingDelaySet sorts ascending by loss rate, then by delay.
type PingDelaySet []CloudflareIPData

func (s PingDelaySet) Len() int      { return len(s) }
func (s PingDelaySet) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s PingDelaySet) Less(i, j int) bool {
	ri, rj := s[i].getLossRate(), s[j].getLossRate()
	if ri != rj {
		return ri < rj
	}
	return s[i].Delay < s[j].Delay
}

func (s PingDelaySet) FilterDelay() (data PingDelaySet) {
	if InputMaxDelay == maxDelay && InputMinDelay == minDelay {
		return s
	}
	for _, v := range s {
		if v.Delay > InputMaxDelay {
			break // already sorted ascending by delay, nothing further qualifies
		}
		if v.Delay < InputMinDelay {
			continue
		}
		data = append(data, v)
	}
	return
}

func (s PingDelaySet) FilterLossRate() (data PingDelaySet) {
	if InputMaxLossRate >= maxLossRate {
		return s
	}
	for _, v := range s {
		if v.getLossRate() > InputMaxLossRate {
			break // already sorted ascending by loss rate
		}
		data = append(data, v)
	}
	return
}

func (s PingDelaySet) FilterJitter() (data PingDelaySet) {
	if InputMaxJitter >= maxJitter {
		return s
	}
	for _, v := range s {
		if v.Jitter <= InputMaxJitter {
			data = append(data, v)
		}
	}
	return
}

// DownloadSpeedSet orders the final result set per ResultSort.
type DownloadSpeedSet []CloudflareIPData

func (s DownloadSpeedSet) Len() int      { return len(s) }
func (s DownloadSpeedSet) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s DownloadSpeedSet) Less(i, j int) bool {
	switch ResultSort {
	case SortDelay:
		return s[i].Delay < s[j].Delay
	case SortScore:
		return s[i].score() > s[j].score()
	default:
		return s[i].DownloadSpeed > s[j].DownloadSpeed
	}
}

func (s DownloadSpeedSet) Print() {
	if NoPrintResult() {
		return
	}
	if len(s) == 0 {
		fmt.Println("\n[info] result set is empty, nothing to print.")
		return
	}

	printNum := PrintNum
	if len(s) < printNum {
		printNum = len(s)
	}

	hasIPv6 := false
	for i := 0; i < printNum; i++ {
		if len(s[i].IP.String()) > 15 {
			hasIPv6 = true
			break
		}
	}

	var headFmt, dataFmt string
	if hasIPv6 {
		headFmt = "%-40s%-6s%-6s%-8s%-10s%-10s%-16s%-6s%-10s\n"
		dataFmt = "%-42s%-8s%-8s%-10s%-12s%-12s%-18s%-8s%-10s\n"
	} else {
		headFmt = "%-16s%-6s%-6s%-8s%-10s%-10s%-16s%-6s%-10s\n"
		dataFmt = "%-18s%-8s%-8s%-10s%-12s%-12s%-18s%-8s%-10s\n"
	}

	Cyan.Printf(headFmt, "IP Address", "Sent", "Recv", "Loss", "Delay", "Jitter", "Speed(MB/s)", "Colo", "ASN")
	for i := 0; i < printNum; i++ {
		r := s[i].toRow()
		fmt.Printf(dataFmt, r[0], r[1], r[2], r[3], r[4]+"ms", r[5]+"ms", r[6], r[7], r[8])
	}

	if !noOutput() {
		fmt.Printf("\nFull results written to %s\n", Output)
	}
}
