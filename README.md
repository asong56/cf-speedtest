# Cloudflare Speedtest

A lightweight, fast, and multi-platform command-line tool written in Go to test the latency and download speed of Cloudflare IP addresses.

## Features
- **TCP Ping & HTTP Ping**: Measure the real latency of Cloudflare Edge nodes.
- **Download Speedtest**: Accurately test the download bandwidth of specific IPs.
- **IPv4 & IPv6 Support**: Reads from `ip.txt` and `ipv6.txt`.
- **CSV Export**: Automatically exports the best-performing IPs to a CSV file for easy integration with proxy clients.
- **Lightweight**: Distributed as a highly compressed, single-binary executable with zero external dependencies.

## Usage

1. Download the latest release for your platform (Windows, macOS, or Linux).
2. Extract the `.zip` archive.
3. Ensure `ip.txt` and/or `ipv6.txt` are in the same directory as the executable.
4. Run the executable from your terminal or command prompt:

```bash
# Linux / macOS
./cf-speedtest

# Windows
cf-speedtest.exe

```

## Building from Source

To build the project yourself, you need [Go](https://golang.org/doc/install) installed.

```bash
git clone [https://github.com/yourusername/cf-speedtest.git](https://github.com/yourusername/cf-speedtest.git)
cd cf-speedtest
go build -ldflags="-s -w" -trimpath -o cf-speedtest main.go

```

## License

MIT License
