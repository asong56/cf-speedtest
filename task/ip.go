package task

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultInputFile   = "ip.txt"
	defaultInputFileV6 = "ipv6.txt"

	// Cloudflare 官方 IP 段列表，内容随时可能更新
	remoteURLv4 = "https://www.cloudflare.com/ips-v4"
	remoteURLv6 = "https://www.cloudflare.com/ips-v6"
)

// fetchRemoteCIDRs 从 Cloudflare 官方拉取最新 IP 段列表。
// 超时 5s，失败返回 nil（调用方负责 fallback）。
func fetchRemoteCIDRs(url string) ([]string, error) {
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// readLocalCIDRs 从本地文件读取 IP 段，文件不存在时返回 nil。
func readLocalCIDRs(filename string) []string {
	f, err := os.Open(filename)
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l != "" && !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}
	return lines
}

var (
	TestAll = false
	IPFile  = defaultInputFile
	IPText  string

	// 修复：用独立 rand.Rand 实例，不再调用全局 rand.Seed（Go 1.20 已废弃）
	rng = rand.New(rand.NewSource(time.Now().UnixNano()))
)

// InitRandSeed 重置随机种子（保留兼容性）
func InitRandSeed() {
	rng = rand.New(rand.NewSource(time.Now().UnixNano()))
}

func isIPv4(ip string) bool {
	return strings.Contains(ip, ".")
}

func randByte(max byte) byte {
	if max == 0 {
		return 0
	}
	return byte(rng.Intn(int(max)))
}

type IPRanges struct {
	ips     []*net.IPAddr
	mask    string
	firstIP net.IP
	ipNet   *net.IPNet
}

func newIPRanges() *IPRanges {
	return &IPRanges{ips: make([]*net.IPAddr, 0)}
}

func (r *IPRanges) fixIP(ip string) string {
	if i := strings.IndexByte(ip, '/'); i < 0 {
		if isIPv4(ip) {
			r.mask = "/32"
		} else {
			r.mask = "/128"
		}
		return ip + r.mask
	}
	r.mask = ip[strings.IndexByte(ip, '/'):]
	return ip
}

func (r *IPRanges) parseCIDR(ip string) {
	var err error
	if r.firstIP, r.ipNet, err = net.ParseCIDR(r.fixIP(ip)); err != nil {
		log.Fatalf("解析 CIDR 失败 [%s]：%v", ip, err)
	}
}

func (r *IPRanges) appendIPv4(d byte) {
	r.ips = append(r.ips, &net.IPAddr{IP: net.IPv4(r.firstIP[12], r.firstIP[13], r.firstIP[14], d)})
}

func (r *IPRanges) appendIP(ip net.IP) {
	r.ips = append(r.ips, &net.IPAddr{IP: ip})
}

func (r *IPRanges) getIPRange() (minIP, hosts byte) {
	minIP = r.firstIP[15] & r.ipNet.Mask[3]
	m := net.IPv4Mask(255, 255, 255, 255)
	for i, v := range r.ipNet.Mask {
		m[i] ^= v
	}
	total, _ := strconv.ParseInt(m.String(), 16, 32)
	if total > 255 {
		hosts = 255
	} else {
		hosts = byte(total)
	}
	return
}

func (r *IPRanges) chooseIPv4() {
	if r.mask == "/32" {
		r.appendIP(r.firstIP)
		return
	}
	minIP, hosts := r.getIPRange()
	for r.ipNet.Contains(r.firstIP) {
		if TestAll {
			for i := byte(0); i <= hosts; i++ {
				r.appendIPv4(minIP + i)
			}
		} else {
			r.appendIPv4(minIP + randByte(hosts))
		}
		r.firstIP[14]++
		if r.firstIP[14] == 0 {
			r.firstIP[13]++
			if r.firstIP[13] == 0 {
				r.firstIP[12]++
			}
		}
	}
}

func (r *IPRanges) chooseIPv6() {
	if r.mask == "/128" {
		r.appendIP(r.firstIP)
		return
	}
	for r.ipNet.Contains(r.firstIP) {
		r.firstIP[15] = randByte(255)
		r.firstIP[14] = randByte(255)

		target := make(net.IP, len(r.firstIP))
		copy(target, r.firstIP)
		r.appendIP(target)

		// 从倒数第三位开始向高位进位
		for i := 13; i >= 0; i-- {
			prev := r.firstIP[i]
			r.firstIP[i] += randByte(255) + 1
			if r.firstIP[i] > prev {
				break
			}
		}
	}
}

// loadIPRanges 解析待测 IP 列表，优先级：
//   1. -ip 参数直接指定（最高优先级，直接返回）
//   2. -f 指定了非默认文件（用户明确指定，直接读本地）
//   3. 默认模式：先尝试从 Cloudflare 官网拉取最新列表，失败则读本地 ip.txt / ipv6.txt
func loadIPRanges() []*net.IPAddr {
	ranges := newIPRanges()

	// ── 优先级 1：-ip 直接指定 ──────────────────────────────────
	if IPText != "" {
		for _, ip := range strings.Split(IPText, ",") {
			ip = strings.TrimSpace(ip)
			if ip == "" {
				continue
			}
			ranges.parseCIDR(ip)
			if isIPv4(ip) {
				ranges.chooseIPv4()
			} else {
				ranges.chooseIPv6()
			}
		}
		return ranges.ips
	}

	// ── 优先级 2：-f 指定了非默认文件 ──────────────────────────
	userSpecifiedFile := IPFile != "" && IPFile != defaultInputFile
	if userSpecifiedFile {
		lines := readLocalCIDRs(IPFile)
		if len(lines) == 0 {
			log.Fatalf("IP 文件为空或不存在：%s", IPFile)
		}
		ranges.parseCIDRLines(lines)
		return ranges.ips
	}

	// ── 优先级 3：默认模式，自动拉取 + 本地兜底 ────────────────
	v4lines := fetchWithFallback(remoteURLv4, defaultInputFile, "IPv4")
	v6lines := fetchWithFallback(remoteURLv6, defaultInputFileV6, "IPv6")

	all := append(v4lines, v6lines...)
	if len(all) == 0 {
		log.Fatal("无法获取任何 IP 段，请检查网络或手动指定 -f 文件。")
	}
	ranges.parseCIDRLines(all)
	return ranges.ips
}

// fetchWithFallback 先拉远程，失败则读本地文件，两者都失败则返回 nil。
func fetchWithFallback(remoteURL, localFile, label string) []string {
	lines, err := fetchRemoteCIDRs(remoteURL)
	if err == nil && len(lines) > 0 {
		fmt.Printf("[IP 列表] %s：从官网获取 %d 条\n", label, len(lines))
		return lines
	}
	// 远程失败，降级到本地
	lines = readLocalCIDRs(localFile)
	if len(lines) > 0 {
		fmt.Printf("[IP 列表] %s：官网不可达，使用本地 %s（%d 条）\n", label, localFile, len(lines))
		return lines
	}
	fmt.Printf("[IP 列表] %s：远程和本地均不可用，跳过\n", label)
	return nil
}

// parseCIDRLines 批量解析并生成待测 IP
func (r *IPRanges) parseCIDRLines(lines []string) {
	for _, line := range lines {
		r.parseCIDR(line)
		if isIPv4(line) {
			r.chooseIPv4()
		} else {
			r.chooseIPv6()
		}
	}
}
