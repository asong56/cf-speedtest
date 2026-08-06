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

var version = "v3.0.0"

func init() {
	var (
		printVersion, printVersionLong bool
		dryRun                         bool
		minDelay, maxDelay, jitter     int
		downloadTime, tcpTimeout       int
		maxLossRate                    float64
		modeStr, sortStr               string
	)

	flag.IntVar(&task.Routines, "n", 200, "")
	flag.IntVar(&task.PingTimes, "t", 4, "")
	flag.StringVar(&modeStr, "m", "tcp", "")
	flag.IntVar(&task.TCPPort, "tp", 443, "")
	flag.IntVar(&tcpTimeout, "ct", 1000, "")

	flag.IntVar(&task.HttpingStatusCode, "code", 0, "")
	flag.StringVar(&task.HttpingCFColo, "colo", "", "")
	flag.StringVar(&task.URL, "url", "https://cf.xiu2.xyz/url", "")

	flag.IntVar(&maxDelay, "tl", 9999, "")
	flag.IntVar(&minDelay, "tll", 0, "")
	flag.Float64Var(&maxLossRate, "tlr", 1.0, "")
	flag.IntVar(&jitter, "tj", 9999, "")
	flag.Float64Var(&task.MinSpeed, "sl", 0.0, "")

	flag.IntVar(&task.TestCount, "dn", 10, "")
	flag.IntVar(&downloadTime, "dt", 10, "")
	flag.IntVar(&task.DownloadThreads, "dc", 1, "")
	flag.BoolVar(&task.Disable, "dd", false, "")

	flag.BoolVar(&task.EnableASN, "asn", false, "")

	flag.StringVar(&task.IPFile, "f", "ip.txt", "")
	flag.StringVar(&task.IPText, "ip", "", "")
	flag.BoolVar(&task.TestAll, "allip", false, "")
	flag.StringVar(&utils.Output, "o", "result.csv", "")
	flag.IntVar(&utils.PrintNum, "p", 10, "")
	flag.StringVar(&sortStr, "sort", "speed", "")

	flag.BoolVar(&utils.Debug, "debug", false, "")
	flag.BoolVar(&printVersion, "v", false, "")
	flag.BoolVar(&printVersionLong, "version", false, "")
	flag.BoolVar(&utils.Quiet, "q", false, "")
	flag.BoolVar(&utils.Quiet, "quiet", false, "")
	flag.BoolVar(&dryRun, "dry-run", false, "")

	flag.Usage = printHelp
	flag.Parse()
	printVersion = printVersion || printVersionLong

	task.Mode = task.ParseMode(modeStr)

	if task.MinSpeed > 0 && maxDelay == 9999 && !utils.Quiet {
		utils.Yellow.Println("[hint] combine -sl with -tl, otherwise the test may run for a long time trying to fill -dn.")
	}
	utils.InputMaxDelay = time.Duration(maxDelay) * time.Millisecond
	utils.InputMinDelay = time.Duration(minDelay) * time.Millisecond
	utils.InputMaxLossRate = float32(maxLossRate)
	utils.InputMaxJitter = time.Duration(jitter) * time.Millisecond
	task.Timeout = time.Duration(downloadTime) * time.Second
	task.TCPConnectTimeout = time.Duration(tcpTimeout) * time.Millisecond
	task.HttpingCFColomap = task.MapColoMap()

	switch sortStr {
	case "delay":
		utils.ResultSort = utils.SortDelay
	case "score":
		utils.ResultSort = utils.SortScore
	default:
		utils.ResultSort = utils.SortSpeed
	}

	if printVersion {
		fmt.Println("CloudflareSpeedTest", version)
		fmt.Print("Checking for updates... ")
		if newVer := checkUpdate(); newVer != "" {
			fmt.Println()
			utils.Yellow.Printf("New version available [%s], download it here:\n", newVer)
			utils.Yellow.Println("https://github.com/XIU2/CloudflareSpeedTest/releases/latest")
		} else {
			utils.Green.Println("Already up to date.")
		}
		os.Exit(0)
	}

	if dryRun {
		printDryRun(modeStr, sortStr, minDelay, maxDelay, jitter, maxLossRate, downloadTime, tcpTimeout)
		os.Exit(0)
	}
}

// printDryRun shows the fully-resolved configuration without probing a single
// IP, so a run can be sanity-checked (flag typos, wrong file, wrong mode)
// before committing to a real test.
func printDryRun(modeStr, sortStr string, minDelay, maxDelay, jitter int, maxLossRate float64, downloadTime, tcpTimeout int) {
	p := fmt.Printf
	p("# dry run: no probes sent, nothing written\n\n")

	source := "remote fetch (cloudflare.com, falls back to " + task.IPFile + ")"
	if task.IPText != "" {
		source = "-ip " + task.IPText
	} else if task.IPFile != "" && task.IPFile != "ip.txt" {
		source = "-f " + task.IPFile
	}

	p("input:\n")
	p("  source        %s\n", source)
	p("  test all IPs  %v\n\n", task.TestAll)

	p("latency test:\n")
	p("  mode          %s (port %d)\n", modeStr, task.TCPPort)
	p("  concurrency   %d\n", task.Routines)
	p("  pings/IP      %d, timeout %dms\n", task.PingTimes, tcpTimeout)
	p("  delay range   %d~%dms, max loss %.2f, max jitter %dms\n\n", minDelay, maxDelay, maxLossRate, jitter)

	p("download test:\n")
	if task.Disable {
		p("  disabled (-dd); results sorted by delay\n\n")
	} else {
		p("  target IPs    %d, duration %ds each, %d thread(s)/IP\n", task.TestCount, downloadTime, task.DownloadThreads)
		p("  min speed     %.2f MB/s\n\n", task.MinSpeed)
	}

	p("output:\n")
	if utils.Output == "" {
		p("  csv file      (disabled)\n")
	} else {
		p("  csv file      %s\n", utils.Output)
	}
	p("  print rows    %d\n", utils.PrintNum)
	p("  sort by       %s\n", sortStr)
}

func main() {
	task.InitRandSeed()

	// Ctrl+C / SIGTERM triggers a graceful stop that still prints whatever was found so far.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !utils.Quiet {
		fmt.Printf("# CloudflareSpeedTest %s\n\n", version)
	}

	pingData := task.NewPing(ctx).Run().FilterDelay().FilterLossRate().FilterJitter()
	speedData := task.TestDownloadSpeed(ctx, pingData)
	task.AnnotateASN(speedData)
	utils.ExportCsv(speedData)
	speedData.Print()
	endPrint()
}

// endPrint keeps the window open when double-clicked on Windows.
func endPrint() {
	if utils.NoPrintResult() || utils.Quiet {
		return
	}
	if runtime.GOOS == "windows" {
		fmt.Print("\nPress Enter or Ctrl+C to exit.")
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

func printHelp() {
	h := utils.Cyan.Printf
	p := fmt.Printf
	dim := utils.White.Printf

	p("\nCloudflareSpeedTest %s\n", version)
	p("Finds the fastest, lowest-latency IPs for Cloudflare and other CDNs.\n")
	p("Project: https://github.com/XIU2/CloudflareSpeedTest\n\n")
	dim("On startup the IP pool is fetched from cloudflare.com/ips-v4 and ips-v6;\n")
	dim("if that fails, it falls back to the local ip.txt / ipv6.txt files.\n")

	p("\n")
	h("[Latency Test]\n")
	p("  -n   <num>    concurrency                 default 200, max 1000\n")
	dim("               on low-power devices (routers) keep this under 50\n")
	p("  -t   <num>    pings per IP                 default 4\n")
	p("  -m   <mode>   probe mode: tcp/icmp/http    default tcp\n")
	dim("               icmp needs root/administrator privileges\n")
	p("  -tp  <port>   TCP port                     default 443\n")
	p("  -ct  <ms>     connect/probe timeout        default 1000\n")
	dim("               lower it (e.g. 500) on poor networks to skip dead IPs faster\n")

	p("\n")
	h("[HTTP Mode Only]\n")
	p("  -code <code>  expected HTTP status code    default 200/301/302\n")
	p("  -colo <list>  keep only these colo codes    comma separated, e.g. HKG,NRT,LAX\n")
	dim("               colo codes are IATA airport codes\n")
	p("  -url  <addr>  probe / download URL          default built-in address\n")
	dim("               self-hosting one avoids relying on a public endpoint\n")

	p("\n")
	h("[Filters]\n")
	p("  -tl  <ms>     max average delay             default 9999 (no filter)\n")
	p("  -tll <ms>     min average delay              default 0 (no filter)\n")
	p("  -tlr <rate>   max loss rate                  default 1.00 (no filter), range 0.00~1.00\n")
	p("  -tj  <ms>     max jitter                     default 9999 (no filter)\n")
	p("  -sl  <speed>  min download speed (MB/s)      default 0 (no filter), pair with -tl\n")
	dim("               stops once -dn IPs qualify; without -tl this can run for a while\n")

	p("\n")
	h("[Download Test]\n")
	p("  -dn <num>     number of IPs to test          default 10\n")
	p("  -dt <sec>     per-IP duration                default 10, avoid going too low\n")
	p("  -dc <num>     concurrent connections per IP   default 1\n")
	p("  -dd           disable download test           results sorted by delay instead\n")

	p("\n")
	h("[Geolocation]\n")
	p("  -asn          resolve origin ASN via DNS      slower, off by default\n")

	p("\n")
	h("[Input / Output]\n")
	p("  -f  <file>    local IP range file             default ip.txt\n")
	dim("               setting this skips the remote fetch; one CIDR per line, # for comments\n")
	p("  -ip <ranges>  test these IP/CIDR directly      comma separated, highest priority\n")
	dim("               example: -ip 1.1.1.1,104.17.0.0/22,2606:4700::/32\n")
	p("  -allip        test every IP in each /24 or /64 range instead of one random sample\n")
	p("  -o  <file>    output CSV path                 default result.csv, \"\" = no file\n")
	dim("               written with a UTF-8 BOM so Excel opens it cleanly on Windows\n")
	p("  -p  <num>     rows printed to the terminal     default 10, 0 = print nothing\n")
	p("  -sort <mode>  result order: speed/delay/score  default speed\n")

	p("\n")
	h("[Other]\n")
	p("  -debug          print verbose debug logs\n")
	p("  -q, -quiet      suppress banners/progress/hints; still prints results and writes csv\n")
	dim("                  pair with -o \"\" and -p 0 for a pure exit-code check\n")
	p("  --dry-run       show the resolved configuration and exit, no probes sent\n")
	p("  -v, --version   print version and check for updates\n")
	p("  -h, --help      show this help\n")

	p("\n")
	h("[Examples]\n")
	p("  # quick scan, only IPs under 200ms\n")
	utils.Green.Println("  CloudflareSpeedTest -tl 200 -dd")
	p("\n")
	p("  # IPs at >= 5 MB/s and <= 150ms, print top 20\n")
	utils.Green.Println("  CloudflareSpeedTest -tl 150 -sl 5 -p 20")
	p("\n")
	p("  # HTTP mode, only HK/Tokyo/LA colos, loss rate <= 10%%\n")
	utils.Green.Println("  CloudflareSpeedTest -m http -colo HKG,NRT,LAX -tlr 0.1")
	p("\n")
	p("  # custom IP range, 500 threads, no CSV output\n")
	utils.Green.Println("  CloudflareSpeedTest -ip 104.16.0.0/13 -n 500 -o \"\"")
	p("\n")
	p("  # low-power device: fewer threads, shorter timeout\n")
	utils.Green.Println("  CloudflareSpeedTest -n 50 -ct 500 -tl 300")
	p("\n")
	p("  # sanity-check flags before a real run\n")
	utils.Green.Println("  CloudflareSpeedTest -m http -colo HKG --dry-run")
	p("\n")
	p("  # scripted use: only the exit code and csv matter\n")
	utils.Green.Println("  CloudflareSpeedTest -q -p 0 -o result.csv")
	p("\n")
}
