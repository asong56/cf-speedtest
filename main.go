package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/task"
	"github.com/XIU2/CloudflareSpeedTest/utils"
)

var version = "v2.3.0"

func init() {
	var printVersion bool
	var minDelay, maxDelay, downloadTime, tcpTimeout int
	var maxLossRate float64

	flag.IntVar(&task.Routines, "n", 200, "")
	flag.IntVar(&task.PingTimes, "t", 4, "")
	flag.IntVar(&task.TestCount, "dn", 10, "")
	flag.IntVar(&downloadTime, "dt", 10, "")
	flag.IntVar(&task.TCPPort, "tp", 443, "")
	flag.IntVar(&tcpTimeout, "ct", 1000, "")
	flag.StringVar(&task.URL, "url", "https://cf.xiu2.xyz/url", "")

	flag.BoolVar(&task.Httping, "httping", false, "")
	flag.IntVar(&task.HttpingStatusCode, "httping-code", 0, "")
	flag.StringVar(&task.HttpingCFColo, "cfcolo", "", "")

	flag.IntVar(&maxDelay, "tl", 9999, "")
	flag.IntVar(&minDelay, "tll", 0, "")
	flag.Float64Var(&maxLossRate, "tlr", 1.0, "")
	flag.Float64Var(&task.MinSpeed, "sl", 0.0, "")

	flag.IntVar(&utils.PrintNum, "p", 10, "")
	flag.StringVar(&task.IPFile, "f", "ip.txt", "")
	flag.StringVar(&task.IPText, "ip", "", "")
	flag.StringVar(&utils.Output, "o", "result.csv", "")

	flag.BoolVar(&task.Disable, "dd", false, "")
	flag.BoolVar(&task.TestAll, "allip", false, "")
	flag.BoolVar(&utils.Debug, "debug", false, "")
	flag.BoolVar(&printVersion, "v", false, "")

	// 完全接管 -h / --help 的输出
	flag.Usage = printHelp
	flag.Parse()

	// 参数应用
	if task.MinSpeed > 0 && time.Duration(maxDelay)*time.Millisecond == utils.InputMaxDelay {
		utils.Yellow.Println("[提示] 使用 -sl 时建议同时指定 -tl，否则可能因凑不满 -dn 数量而持续测速。")
	}
	utils.InputMaxDelay = time.Duration(maxDelay) * time.Millisecond
	utils.InputMinDelay = time.Duration(minDelay) * time.Millisecond
	utils.InputMaxLossRate = float32(maxLossRate)
	task.Timeout = time.Duration(downloadTime) * time.Second
	task.TCPConnectTimeout = time.Duration(tcpTimeout) * time.Millisecond
	task.HttpingCFColomap = task.MapColoMap()

	if printVersion {
		fmt.Println("CloudflareSpeedTest", version)
		fmt.Print("检查更新中... ")
		if newVer := checkUpdate(); newVer != "" {
			fmt.Println()
			utils.Yellow.Printf("发现新版本 [%s]，请前往以下地址下载更新：\n", newVer)
			utils.Yellow.Println("https://github.com/XIU2/CloudflareSpeedTest/releases/latest")
		} else {
			utils.Green.Println("当前已是最新版本。")
		}
		os.Exit(0)
	}
}

func main() {
	task.InitRandSeed()

	// 监听 Ctrl+C / SIGTERM，优雅退出并保留已有结果
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("# CloudflareSpeedTest %s\n\n", version)

	pingData := task.NewPing(ctx).Run().FilterDelay().FilterLossRate()
	speedData := task.TestDownloadSpeed(ctx, pingData)
	utils.ExportCsv(speedData)
	speedData.Print()
	endPrint()
}

// endPrint 在 Windows 双击运行时防止窗口立即关闭
func endPrint() {
	if utils.NoPrintResult() {
		return
	}
	if runtime.GOOS == "windows" {
		fmt.Print("\n按下 回车键 或 Ctrl+C 退出。")
		fmt.Scanln()
	}
}

func checkUpdate() string {
	client := http.Client{Timeout: 10 * time.Second}
	res, err := client.Get("https://api.xiu2.xyz/ver/cloudflarespeedtest.txt")
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if newVer := string(body); newVer != version {
		return newVer
	}
	return ""
}

// printHelp 输出结构清晰的帮助文本
func printHelp() {
	// 颜色别名，方便下面使用
	h := utils.Cyan.PrintfFunc()   // 分组标题
	p := fmt.Printf               // 普通行
	dim := utils.White.PrintfFunc() // 说明文字

	p("\nCloudflareSpeedTest %s\n", version)
	p("测试 Cloudflare 及各 CDN 所有 IP 的延迟和速度，找出最快节点。\n")
	p("项目地址：https://github.com/XIU2/CloudflareSpeedTest\n")
	p("\n")
	dim("启动时自动从 cloudflare.com/ips-v4 和 ips-v6 拉取最新 IP 段；\n")
	dim("网络不可达时自动降级读取本地 ip.txt / ipv6.txt 作为兜底。\n")

	// ── 延迟测速 ──────────────────────────────────────────────────
	p("\n")
	h("【延迟测速】\n")
	p("  -n  <数量>   并发线程数              默认 200，最大 1000\n")
	dim("               路由器等低性能设备建议设为 50 以下\n")
	p("  -t  <次数>   单 IP 测速次数          默认 4 次\n")
	p("  -tp <端口>   测速端口                默认 443\n")
	p("  -ct <毫秒>   TCP 连接超时            默认 1000 ms\n")
	dim("               网络较差时适当调小（如 500）可加速跳过超时节点\n")

	// ── HTTPing 模式 ──────────────────────────────────────────────
	p("\n")
	h("【HTTPing 模式】（默认为 TCPing）\n")
	p("  -httping              切换为 HTTP 延迟测速\n")
	p("  -httping-code <码>    有效 HTTP 状态码      默认 200/301/302\n")
	p("  -cfcolo <地区码>      只保留指定地区的节点   仅 HTTPing 可用\n")
	dim("               多个地区用英文逗号分隔，如 HKG,NRT,LAX,SJC\n")
	dim("               地区码为 IATA 机场三字码\n")
	p("  -url <地址>           测速/下载地址          默认使用内置地址\n")
	dim("               建议自建，避免因公共地址不稳定影响结果\n")

	// ── 过滤条件 ──────────────────────────────────────────────────
	p("\n")
	h("【过滤条件】\n")
	p("  -tl  <毫秒>   平均延迟上限    默认 9999 ms（不过滤）\n")
	p("  -tll <毫秒>   平均延迟下限    默认 0 ms（不过滤）\n")
	p("  -tlr <比率>   丢包率上限      默认 1.00（不过滤），范围 0.00~1.00\n")
	dim("               0 = 完全不允许丢包；0.2 = 丢包率不超过 20%%\n")
	p("  -sl  <速度>   下载速度下限    默认 0 MB/s（不过滤），建议搭配 -tl\n")
	dim("               凑够 -dn 数量后自动停止，未指定 -tl 时可能持续很久\n")

	// ── 下载测速 ──────────────────────────────────────────────────
	p("\n")
	h("【下载测速】\n")
	p("  -dn <数量>    下载测速的 IP 数量     默认 10 个\n")
	p("  -dt <秒>      单 IP 下载时长         默认 10 秒，不宜过短\n")
	p("  -dd           禁用下载测速           结果改为按延迟排序\n")

	// ── 输入 / 输出 ───────────────────────────────────────────────
	p("\n")
	h("【输入 / 输出】\n")
	p("  -f  <文件>    指定本地 IP 段文件      默认 ip.txt\n")
	dim("               指定此参数后跳过远程拉取，直接读取本地文件\n")
	dim("               文件每行一个 CIDR，支持 # 注释行\n")
	p("  -ip <IP段>    直接指定 IP 段         多个以逗号分隔，优先级最高\n")
	dim("               例：-ip 1.1.1.1,104.17.0.0/22,2606:4700::/32\n")
	p("  -o  <文件>    输出 CSV 文件          默认 result.csv\n")
	dim("               留空则不写文件：-o \"\"\n")
	dim("               文件含 UTF-8 BOM，Windows Excel 可直接打开\n")
	p("  -p  <数量>    终端打印行数           默认 10，0 = 不打印\n")
	p("  -allip        测速 /24 段内所有 IP   默认每段随机取一个\n")

	// ── 其他 ──────────────────────────────────────────────────────
	p("\n")
	h("【其他】\n")
	p("  -debug   输出详细调试日志\n")
	p("  -v       显示版本并检查更新\n")
	p("  -h       显示此帮助\n")

	// ── 常用示例 ──────────────────────────────────────────────────
	p("\n")
	h("【常用示例】\n")
	p("  # 快速扫描，只要延迟 200ms 以内的节点\n")
	utils.Green.Println("  CloudflareSpeedTest -tl 200 -dd")
	p("\n")
	p("  # 找下载速度 ≥ 5 MB/s 且延迟 ≤ 150ms 的节点，输出前 20 条\n")
	utils.Green.Println("  CloudflareSpeedTest -tl 150 -sl 5 -p 20")
	p("\n")
	p("  # HTTPing 模式，只保留香港/东京/洛杉矶节点，丢包率 ≤ 10%%\n")
	utils.Green.Println("  CloudflareSpeedTest -httping -cfcolo HKG,NRT,LAX -tlr 0.1")
	p("\n")
	p("  # 测试自定义 IP 段，500 线程，不写文件\n")
	utils.Green.Println("  CloudflareSpeedTest -ip 104.16.0.0/13 -n 500 -o \"\"")
	p("\n")
	p("  # 低性能设备（如路由器），降低线程和超时\n")
	utils.Green.Println("  CloudflareSpeedTest -n 50 -ct 500 -tl 300")
	p("\n")
}
