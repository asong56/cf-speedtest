# Cloudflare Speedtest

A lightweight, fast, multi-platform command-line tool written in Go to find
the fastest, lowest-latency IPs for Cloudflare (or any CDN) and measure their
real download speed.

## Features
- **TCP / ICMP / HTTP probing** (`-m tcp|icmp|http`) with loss-rate and jitter stats.
- **Two-stage funnel**: cheap latency probe first, real HTTP download test only on the survivors.
- **Multi-threaded download test** (`-dc`) to reflect an IP's real upper-bound throughput.
- **Colo / IATA detection** from CDN response headers, plus optional **ASN lookup** (`-asn`) via DNS.
- **Flexible filtering & sorting**: delay, jitter, loss rate, min speed, `-sort speed|delay|score`.
- **IPv4 & IPv6 support**, CIDR sampling or full-range scan (`-allip`).
- **CSV export** with a UTF-8 BOM for direct use in Excel or proxy client configs.
- **Zero external dependencies** — pure Go standard library, so the binary stays as small as `go build` can make it.

## Usage

1. Download the latest release for your platform (Windows, macOS, or Linux).
2. Extract the archive.
3. Ensure `ip.txt` and/or `ipv6.txt` are in the same directory as the executable (used only as a fallback if the live Cloudflare list can't be fetched).
4. Run it:

```bash
# Linux / macOS
./cf-speedtest

# Windows
cf-speedtest.exe
```

Run `cf-speedtest -h` for the full flag reference. Quick examples:

```bash
# quick scan, only IPs under 200ms, skip the download test
cf-speedtest -tl 200 -dd

# IPs at >= 5 MB/s and <= 150ms, print the top 20
cf-speedtest -tl 150 -sl 5 -p 20

# HTTP mode, only Hong Kong/Tokyo/LA colos, loss rate <= 10%
cf-speedtest -m http -colo HKG,NRT,LAX -tlr 0.1

# ICMP mode needs elevated privileges
sudo cf-speedtest -m icmp
```

## Building from Source

Requires [Go](https://golang.org/doc/install) 1.21+. The module has no
third-party dependencies, so the build works fully offline.

```bash
git clone https://github.com/yourusername/cf-speedtest.git
cd cf-speedtest
CGO_ENABLED=0 go build -ldflags="-s -w" -trimpath -o cf-speedtest .
```

For the smallest possible binary, compress the result with [UPX](https://upx.github.io/)
(already wired up in `.github/workflows/release.yml`):

```bash
upx --best --lzma cf-speedtest
```

## License

MIT License
