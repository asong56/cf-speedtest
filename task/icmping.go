package task

import (
	"encoding/binary"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/utils"
)

const (
	icmpEchoRequestV4 = 8
	icmpEchoReplyV4   = 0
	icmpEchoRequestV6 = 128
	icmpEchoReplyV6   = 129
)

var (
	icmpSeq      uint32
	icmpID       = uint16(os.Getpid() & 0xffff)
	icmpWarnOnce sync.Once
)

func (p *Ping) icmping(ip *net.IPAddr) ([]time.Duration, string) {
	var rtts []time.Duration
	for i := 0; i < PingTimes; i++ {
		d, err := icmpEcho(ip, TCPConnectTimeout)
		if err != nil {
			if isICMPPermissionError(err) {
				icmpWarnOnce.Do(func() {
					utils.Red.Println("[error] ICMP mode needs raw-socket privileges; run as root/administrator, or use -m tcp/http instead")
				})
				return nil, ""
			}
			continue
		}
		rtts = append(rtts, d)
	}
	return rtts, ""
}

// icmpEcho sends one raw ICMP echo request and blocks for the matching reply or the timeout.
func icmpEcho(ip *net.IPAddr, timeout time.Duration) (time.Duration, error) {
	v6 := !isIPv4(ip.String())
	network := "ip4:icmp"
	if v6 {
		network = "ip6:ipv6-icmp"
	}
	conn, err := net.DialIP(network, nil, ip)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	seq := uint16(atomic.AddUint32(&icmpSeq, 1))
	pkt := buildICMPEcho(v6, icmpID, seq)

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return 0, err
	}
	start := time.Now()
	if _, err := conn.Write(pkt); err != nil {
		return 0, err
	}

	buf := make([]byte, 512)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return 0, err
		}
		body := buf[:n]
		if !v6 {
			// raw IPv4 sockets return the IP header too; skip it to reach the ICMP body
			if n < 1 {
				continue
			}
			hl := int(buf[0]&0x0f) * 4
			if n < hl+8 {
				continue
			}
			body = buf[hl:n]
		}
		if len(body) < 8 {
			continue
		}
		if binary.BigEndian.Uint16(body[4:6]) != icmpID || binary.BigEndian.Uint16(body[6:8]) != seq {
			continue
		}
		typ := body[0]
		if (!v6 && typ == icmpEchoReplyV4) || (v6 && typ == icmpEchoReplyV6) {
			return time.Since(start), nil
		}
	}
}

func buildICMPEcho(v6 bool, id, seq uint16) []byte {
	typ := byte(icmpEchoRequestV4)
	if v6 {
		typ = icmpEchoRequestV6
	}
	pkt := make([]byte, 16)
	pkt[0] = typ
	binary.BigEndian.PutUint16(pkt[4:6], id)
	binary.BigEndian.PutUint16(pkt[6:8], seq)
	binary.BigEndian.PutUint64(pkt[8:16], uint64(time.Now().UnixNano()))
	if !v6 {
		// IPv6 raw sockets have the kernel fill the checksum via IPV6_CHECKSUM
		binary.BigEndian.PutUint16(pkt[2:4], icmpChecksum(pkt))
	}
	return pkt
}

func icmpChecksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func isICMPPermissionError(err error) bool {
	return os.IsPermission(err) ||
		strings.Contains(err.Error(), "operation not permitted") ||
		strings.Contains(err.Error(), "permission denied")
}
