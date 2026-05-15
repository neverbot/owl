// Package host parses Linux /proc files and emits node_exporter-compatible
// samples. Parsers accept an io/fs.FS so tests can run against synthetic
// trees on every OS, including macOS where /proc does not exist.
package host

import (
	"bufio"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/neverbot/owl/internal/storage"
)

// USER_HZ on virtually every Linux kernel build for x86_64 / arm64.
// Going through cgo or a syscall just to confirm it is overkill at this
// scale; if a build appears in the wild with a different value, the CPU
// numbers will be off by a constant factor but still relative.
const userHz = 100.0

// ParseStat reads /proc/stat from fsys and returns per-CPU
// `node_cpu_seconds_total{cpu,mode}` samples plus `node_boot_time_seconds`.
// The aggregate `cpu` line is ignored — node_exporter does the same.
func ParseStat(fsys fs.FS, now int64) ([]storage.Sample, error) {
	data, err := readAll(fsys, "stat")
	if err != nil {
		return nil, err
	}
	modes := []string{"user", "nice", "system", "idle", "iowait", "irq", "softirq", "steal", "guest", "guest_nice"}
	var out []storage.Sample
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		switch {
		case fields[0] == "btime" && len(fields) >= 2:
			if ts, err := strconv.ParseFloat(fields[1], 64); err == nil {
				out = append(out, storage.Sample{
					Metric: "node_boot_time_seconds",
					Labels: map[string]string{"job": "host"},
					TS:     now,
					Value:  ts,
				})
			}
		case strings.HasPrefix(fields[0], "cpu") && fields[0] != "cpu":
			cpu := strings.TrimPrefix(fields[0], "cpu")
			for i, mode := range modes {
				idx := i + 1
				if idx >= len(fields) {
					break
				}
				v, err := strconv.ParseFloat(fields[idx], 64)
				if err != nil {
					continue
				}
				out = append(out, storage.Sample{
					Metric: "node_cpu_seconds_total",
					Labels: map[string]string{"job": "host", "cpu": cpu, "mode": mode},
					TS:     now,
					Value:  v / userHz,
				})
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan stat: %w", err)
	}
	return out, nil
}

// memField captures one `MemTotal: 12345 kB`-style line.
type memField struct {
	key, metric string
}

var memFields = []memField{
	{"MemTotal", "node_memory_MemTotal_bytes"},
	{"MemFree", "node_memory_MemFree_bytes"},
	{"MemAvailable", "node_memory_MemAvailable_bytes"},
	{"Buffers", "node_memory_Buffers_bytes"},
	{"Cached", "node_memory_Cached_bytes"},
	{"SwapTotal", "node_memory_SwapTotal_bytes"},
	{"SwapFree", "node_memory_SwapFree_bytes"},
}

// ParseMeminfo reads /proc/meminfo and emits a subset of node_memory_*
// gauges. Other fields are ignored — adding new metrics is a matter of
// extending `memFields`.
func ParseMeminfo(fsys fs.FS, now int64) ([]storage.Sample, error) {
	data, err := readAll(fsys, "meminfo")
	if err != nil {
		return nil, err
	}
	want := make(map[string]string, len(memFields))
	for _, f := range memFields {
		want[f.key] = f.metric
	}

	var out []storage.Sample
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		metric, ok := want[key]
		if !ok {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(line[colon+1:]))
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue
		}
		// Convert kB → bytes when the line carries that suffix.
		if len(fields) >= 2 && strings.EqualFold(fields[1], "kB") {
			v *= 1024
		}
		out = append(out, storage.Sample{
			Metric: metric,
			Labels: map[string]string{"job": "host"},
			TS:     now,
			Value:  v,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan meminfo: %w", err)
	}
	return out, nil
}

// ParseLoadavg reads /proc/loadavg and emits node_load{1,5,15}.
func ParseLoadavg(fsys fs.FS, now int64) ([]storage.Sample, error) {
	data, err := readAll(fsys, "loadavg")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(data)
	if len(fields) < 3 {
		return nil, fmt.Errorf("loadavg: short line %q", data)
	}
	out := make([]storage.Sample, 0, 3)
	names := []string{"node_load1", "node_load5", "node_load15"}
	for i, name := range names {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return nil, fmt.Errorf("loadavg field %d: %w", i, err)
		}
		out = append(out, storage.Sample{
			Metric: name,
			Labels: map[string]string{"job": "host"},
			TS:     now,
			Value:  v,
		})
	}
	return out, nil
}

// isVirtualNet reports whether a network device name is a tunnel /
// virtual interface we don't want to chart by default. Matches the
// kinds of things that show up on a stock Linux server but rarely
// carry real traffic for a small-cluster operator.
func isVirtualNet(name string) bool {
	prefixes := []string{"ip6gre", "ip6tnl", "ip6_vti", "ip_vti", "gretap", "gre", "erspan", "sit", "tunl"}
	for _, p := range prefixes {
		if name == p+"0" || (len(name) > len(p) && name[:len(p)] == p && (name[len(p)] >= '0' && name[len(p)] <= '9')) {
			return true
		}
	}
	return false
}

// ParseNetDev reads /proc/net/dev and emits rx/tx bytes + packets per
// interface. Loopback (`lo`) is included; node_exporter does the same
// by default. Pure-virtual tunnel devices are filtered out.
func ParseNetDev(fsys fs.FS, now int64) ([]storage.Sample, error) {
	data, err := readAll(fsys, "net/dev")
	if err != nil {
		return nil, err
	}
	var out []storage.Sample
	sc := bufio.NewScanner(strings.NewReader(data))
	lineNo := 0
	for sc.Scan() {
		lineNo++
		if lineNo <= 2 {
			continue // header rows
		}
		line := sc.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		dev := strings.TrimSpace(line[:colon])
		if isVirtualNet(dev) {
			continue
		}
		fields := strings.Fields(line[colon+1:])
		if len(fields) < 16 {
			continue
		}
		// Layout: rx bytes packets errs drop fifo frame compressed multicast
		//         tx bytes packets errs drop fifo colls carrier compressed
		rxBytes, _ := strconv.ParseFloat(fields[0], 64)
		rxPackets, _ := strconv.ParseFloat(fields[1], 64)
		rxErrs, _ := strconv.ParseFloat(fields[2], 64)
		txBytes, _ := strconv.ParseFloat(fields[8], 64)
		txPackets, _ := strconv.ParseFloat(fields[9], 64)
		txErrs, _ := strconv.ParseFloat(fields[10], 64)
		lbl := map[string]string{"job": "host", "device": dev}
		out = append(out,
			storage.Sample{Metric: "node_network_receive_bytes_total", Labels: lbl, TS: now, Value: rxBytes},
			storage.Sample{Metric: "node_network_receive_packets_total", Labels: lbl, TS: now, Value: rxPackets},
			storage.Sample{Metric: "node_network_receive_errs_total", Labels: lbl, TS: now, Value: rxErrs},
			storage.Sample{Metric: "node_network_transmit_bytes_total", Labels: lbl, TS: now, Value: txBytes},
			storage.Sample{Metric: "node_network_transmit_packets_total", Labels: lbl, TS: now, Value: txPackets},
			storage.Sample{Metric: "node_network_transmit_errs_total", Labels: lbl, TS: now, Value: txErrs},
		)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan net/dev: %w", err)
	}
	return out, nil
}

// ParseDiskstats reads /proc/diskstats and emits read/write bytes + IOs
// per device. Per the kernel docs, sector size is 512 bytes regardless
// of the underlying device sector size.
func ParseDiskstats(fsys fs.FS, now int64) ([]storage.Sample, error) {
	data, err := readAll(fsys, "diskstats")
	if err != nil {
		return nil, err
	}
	const sectorBytes = 512.0
	var out []storage.Sample
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 14 {
			continue
		}
		dev := fields[2]
		// Skip virtual / pseudo devices to keep cardinality manageable
		// for a small-cluster operator. nbd* are Docker Desktop VM
		// internals; loop/ram/dm-/zram/sr/fd are kernel pseudo-devices.
		skip := false
		for _, p := range []string{"loop", "ram", "dm-", "zram", "nbd", "sr", "fd"} {
			if strings.HasPrefix(dev, p) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		readsCompleted, _ := strconv.ParseFloat(fields[3], 64)
		sectorsRead, _ := strconv.ParseFloat(fields[5], 64)
		writesCompleted, _ := strconv.ParseFloat(fields[7], 64)
		sectorsWritten, _ := strconv.ParseFloat(fields[9], 64)
		lbl := map[string]string{"job": "host", "device": dev}
		out = append(out,
			storage.Sample{Metric: "node_disk_read_bytes_total", Labels: lbl, TS: now, Value: sectorsRead * sectorBytes},
			storage.Sample{Metric: "node_disk_written_bytes_total", Labels: lbl, TS: now, Value: sectorsWritten * sectorBytes},
			storage.Sample{Metric: "node_disk_reads_completed_total", Labels: lbl, TS: now, Value: readsCompleted},
			storage.Sample{Metric: "node_disk_writes_completed_total", Labels: lbl, TS: now, Value: writesCompleted},
		)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan diskstats: %w", err)
	}
	return out, nil
}

// readAll reads the whole file at name from fsys.
func readAll(fsys fs.FS, name string) (string, error) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// nowMs returns time.Now in milliseconds — extracted so tests can stub
// it if needed via the optional NowFunc field on Collector.
func nowMs() int64 { return time.Now().UnixMilli() }
