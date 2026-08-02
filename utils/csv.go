package utils

import (
	"encoding/csv"
	"fmt"
	"log"
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
)

var (
	InputMaxDelay    = maxDelay
	InputMinDelay    = minDelay
	InputMaxLossRate = maxLossRate
	Output           = defaultOutput
	PrintNum         = 10
	Debug            = false
)

func NoPrintResult() bool { return PrintNum == 0 }
func noOutput() bool      { return Output == "" || Output == " " }

// PingData 单次延迟测速结果
type PingData struct {
	IP       *net.IPAddr
	Sended   int
	Received int
	Delay    time.Duration
	Colo     string
}

// CloudflareIPData 完整测速结果（延迟 + 下载速度）
type CloudflareIPData struct {
	*PingData
	lossRate      float32
	DownloadSpeed float64
}

func (cf *CloudflareIPData) getLossRate() float32 {
	if cf.lossRate == 0 && cf.Sended > 0 {
		cf.lossRate = float32(cf.Sended-cf.Received) / float32(cf.Sended)
	}
	return cf.lossRate
}

func (cf *CloudflareIPData) toRow() []string {
	colo := cf.Colo
	if colo == "" {
		colo = "N/A"
	}
	return []string{
		cf.IP.String(),
		strconv.Itoa(cf.Sended),
		strconv.Itoa(cf.Received),
		strconv.FormatFloat(float64(cf.getLossRate()), 'f', 2, 32),
		strconv.FormatFloat(cf.Delay.Seconds()*1000, 'f', 2, 32),
		strconv.FormatFloat(cf.DownloadSpeed/1024/1024, 'f', 2, 32),
		colo,
	}
}

// ExportCsv 将结果写入 CSV。
// 写入 UTF-8 BOM，确保 Windows Excel 直接打开不乱码。
func ExportCsv(data []CloudflareIPData) {
	if noOutput() || len(data) == 0 {
		return
	}
	fp, err := os.Create(Output)
	if err != nil {
		log.Fatalf("创建文件 [%s] 失败：%v", Output, err)
	}
	defer fp.Close()

	// UTF-8 BOM
	fp.WriteString("\xEF\xBB\xBF")

	w := csv.NewWriter(fp)
	_ = w.Write([]string{"IP 地址", "已发送", "已接收", "丢包率", "平均延迟(ms)", "下载速度(MB/s)", "地区码"})
	for _, v := range data {
		_ = w.Write(v.toRow())
	}
	w.Flush()
	if err := w.Error(); err != nil {
		log.Fatalf("写入 CSV 失败：%v", err)
	}
}

// ---- 排序类型 ----

// PingDelaySet 按丢包率升序、延迟升序排列
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

// FilterDelay 过滤延迟不达标的 IP。
//
// 修复原版 Bug：原条件 `InputMaxDelay > maxDelay || InputMinDelay < minDelay`
// 因为 maxDelay=9999ms、minDelay=0，用户输入的值永远在此范围内，
// 导致该分支永远成立、过滤逻辑永远被跳过。
// 正确做法：均为默认值时才跳过；否则执行过滤。
func (s PingDelaySet) FilterDelay() (data PingDelaySet) {
	if InputMaxDelay == maxDelay && InputMinDelay == minDelay {
		return s
	}
	for _, v := range s {
		if v.Delay > InputMaxDelay {
			break // 已按延迟升序，后续必然超出上限
		}
		if v.Delay < InputMinDelay {
			continue
		}
		data = append(data, v)
	}
	return
}

// FilterLossRate 过滤丢包率不达标的 IP
func (s PingDelaySet) FilterLossRate() (data PingDelaySet) {
	if InputMaxLossRate >= maxLossRate {
		return s
	}
	for _, v := range s {
		if v.getLossRate() > InputMaxLossRate {
			break // 已按丢包率升序
		}
		data = append(data, v)
	}
	return
}

// DownloadSpeedSet 按下载速度降序排列
type DownloadSpeedSet []CloudflareIPData

func (s DownloadSpeedSet) Len() int           { return len(s) }
func (s DownloadSpeedSet) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s DownloadSpeedSet) Less(i, j int) bool { return s[i].DownloadSpeed > s[j].DownloadSpeed }

// Print 格式化打印测速结果
func (s DownloadSpeedSet) Print() {
	if NoPrintResult() {
		return
	}
	if len(s) == 0 {
		fmt.Println("\n[信息] 测速结果数量为 0，跳过打印。")
		return
	}

	printNum := PrintNum
	if len(s) < printNum {
		printNum = len(s)
	}

	// 检测是否含 IPv6，自动调整列宽
	hasIPv6 := false
	for i := 0; i < printNum; i++ {
		if len(s[i].IP.String()) > 15 {
			hasIPv6 = true
			break
		}
	}

	var headFmt, dataFmt string
	if hasIPv6 {
		headFmt = "%-40s%-6s%-6s%-8s%-12s%-16s%-6s\n"
		dataFmt = "%-42s%-8s%-8s%-10s%-14s%-18s%-8s\n"
	} else {
		headFmt = "%-16s%-6s%-6s%-8s%-12s%-16s%-6s\n"
		dataFmt = "%-18s%-8s%-8s%-10s%-14s%-18s%-8s\n"
	}

	Cyan.Printf(headFmt, "IP 地址", "发送", "接收", "丢包率", "平均延迟", "下载速度(MB/s)", "地区")
	for i := 0; i < printNum; i++ {
		r := s[i].toRow()
		fmt.Printf(dataFmt, r[0], r[1], r[2], r[3], r[4]+"ms", r[5], r[6])
	}

	if !noOutput() {
		fmt.Printf("\n完整结果已写入 %s，可用记事本或表格软件查看。\n", Output)
	}
}
