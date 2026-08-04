package task

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/utils"
)

var (
	EnableASN bool
	asnCache  sync.Map
)

// ResolveASN looks up the origin ASN for ip via a Team Cymru DNS query.
// Returns "" when disabled, cached-empty, or unresolved.
func ResolveASN(ip net.IP) string {
	if !EnableASN {
		return ""
	}
	key := ip.String()
	if v, ok := asnCache.Load(key); ok {
		return v.(string)
	}
	query := cymruQuery(ip)
	if query == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var r net.Resolver
	txts, err := r.LookupTXT(ctx, query)
	if err != nil || len(txts) == 0 {
		asnCache.Store(key, "")
		return ""
	}
	fields := strings.Split(txts[0], "|")
	asn := ""
	if len(fields) > 0 {
		asn = "AS" + strings.TrimSpace(fields[0])
	}
	asnCache.Store(key, asn)
	return asn
}

func cymruQuery(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d.origin.asn.cymru.com", v4[3], v4[2], v4[1], v4[0])
	}
	v6 := ip.To16()
	if v6 == nil {
		return ""
	}
	nibbles := make([]string, 0, 32)
	for i := len(v6) - 1; i >= 0; i-- {
		b := v6[i]
		nibbles = append(nibbles, strconv.FormatUint(uint64(b&0x0f), 16))
		nibbles = append(nibbles, strconv.FormatUint(uint64(b>>4), 16))
	}
	return strings.Join(nibbles, ".") + ".origin6.asn.cymru.com"
}

// AnnotateASN fills in the ASN field for each result, capped at 20 concurrent DNS lookups.
func AnnotateASN(data []utils.CloudflareIPData) {
	if !EnableASN || len(data) == 0 {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 20)
	for i := range data {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			data[idx].ASN = ResolveASN(data[idx].IP.IP)
		}(i)
	}
	wg.Wait()
}
