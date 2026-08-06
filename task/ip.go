package task

import (
	"bufio"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/utils"
)

const (
	defaultInputFile   = "ip.txt"
	defaultInputFileV6 = "ipv6.txt"
	remoteURLv4        = "https://www.cloudflare.com/ips-v4"
	remoteURLv6        = "https://www.cloudflare.com/ips-v6"
)

var (
	TestAll = false
	IPFile  = defaultInputFile
	IPText  string

	rng = rand.New(rand.NewSource(time.Now().UnixNano()))
)

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

func (r *IPRanges) parseCIDR(ip string) error {
	var err error
	r.firstIP, r.ipNet, err = net.ParseCIDR(r.fixIP(ip))
	if err != nil {
		return fmt.Errorf("invalid CIDR %q: %w", ip, err)
	}
	return nil
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

		for i := 13; i >= 0; i-- {
			prev := r.firstIP[i]
			r.firstIP[i] += randByte(255) + 1
			if r.firstIP[i] > prev {
				break
			}
		}
	}
}

func (r *IPRanges) parseCIDRLines(lines []string) {
	for _, line := range lines {
		if err := r.parseCIDR(line); err != nil {
			fmt.Fprintln(os.Stderr, "[warn]", err)
			continue
		}
		if isIPv4(line) {
			r.chooseIPv4()
		} else {
			r.chooseIPv6()
		}
	}
}

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

func fetchWithFallback(remoteURL, localFile, label string) []string {
	lines, err := fetchRemoteCIDRs(remoteURL)
	if err == nil && len(lines) > 0 {
		if !utils.Quiet {
			fmt.Printf("[ip pool] %s: fetched %d ranges from cloudflare.com\n", label, len(lines))
		}
		return lines
	}
	lines = readLocalCIDRs(localFile)
	if len(lines) > 0 {
		if !utils.Quiet {
			fmt.Printf("[ip pool] %s: remote unreachable, using local %s (%d ranges)\n", label, localFile, len(lines))
		}
		return lines
	}
	if !utils.Quiet {
		fmt.Printf("[ip pool] %s: remote and local both unavailable, skipped\n", label)
	}
	return nil
}

// loadIPRanges resolves the IP pool. Priority: -ip > -f (non-default) > remote fetch with local fallback.
func loadIPRanges() []*net.IPAddr {
	ranges := newIPRanges()

	if IPText != "" {
		var lines []string
		for _, ip := range strings.Split(IPText, ",") {
			ip = strings.TrimSpace(ip)
			if ip != "" {
				lines = append(lines, ip)
			}
		}
		ranges.parseCIDRLines(lines)
		return ranges.ips
	}

	if IPFile != "" && IPFile != defaultInputFile {
		lines := readLocalCIDRs(IPFile)
		if len(lines) == 0 {
			fmt.Fprintf(os.Stderr, "[error] IP file is empty or missing: %s\n", IPFile)
			os.Exit(1)
		}
		ranges.parseCIDRLines(lines)
		return ranges.ips
	}

	v4lines := fetchWithFallback(remoteURLv4, defaultInputFile, "IPv4")
	v6lines := fetchWithFallback(remoteURLv6, defaultInputFileV6, "IPv6")

	all := append(v4lines, v6lines...)
	if len(all) == 0 {
		fmt.Fprintln(os.Stderr, "[error] no IP ranges available, check network or specify -f manually")
		os.Exit(1)
	}
	ranges.parseCIDRLines(all)
	return ranges.ips
}
