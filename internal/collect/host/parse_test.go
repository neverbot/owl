package host

import (
	"testing"
	"testing/fstest"

	"github.com/neverbot/owl/internal/storage"
)

const sampleStat = `cpu  100 0 200 800 0 0 0 0 0 0
cpu0 50 0 100 400 0 0 0 0 0 0
cpu1 50 0 100 400 0 0 0 0 0 0
intr 12345
ctxt 67890
btime 1700000000
processes 5432
`

const sampleMeminfo = `MemTotal:        2048000 kB
MemFree:          512000 kB
MemAvailable:    1024000 kB
Buffers:          128000 kB
Cached:           256000 kB
SwapTotal:       1048576 kB
SwapFree:        1048576 kB
Dirty:              4096 kB
Slab:              16384 kB
`

const sampleLoadavg = `0.42 0.55 0.61 2/345 6789
`

const sampleNetDev = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1234       10    0    0    0     0          0         0    1234       10    0    0    0     0       0          0
  eth0: 567890    200    1    0    0     0          0         0  234567      150    2    0    0     0       0          0
`

const sampleDiskstats = `   8       0 sda 1000 0 2048 50 500 0 4096 30 0 100 80 0 0 0 0 0 0
   8      16 sdb 2000 0 4096 100 1000 0 8192 60 0 200 160 0 0 0 0 0 0
   7       0 loop0 1 0 8 0 0 0 0 0 0 0 0 0 0 0 0 0 0
 253       0 dm-0 50 0 400 5 25 0 200 5 0 10 10 0 0 0 0 0 0
`

func makeProc() fstest.MapFS {
	return fstest.MapFS{
		"stat":      {Data: []byte(sampleStat)},
		"meminfo":   {Data: []byte(sampleMeminfo)},
		"loadavg":   {Data: []byte(sampleLoadavg)},
		"net/dev":   {Data: []byte(sampleNetDev)},
		"diskstats": {Data: []byte(sampleDiskstats)},
	}
}

func indexByMetricLabel(samples []storage.Sample, labelKey, labelValue string) map[string]float64 {
	out := map[string]float64{}
	for _, s := range samples {
		if labelKey == "" || s.Labels[labelKey] == labelValue {
			out[s.Metric] = s.Value
		}
	}
	return out
}

func TestParseStatEmitsPerCPUAndBootTime(t *testing.T) {
	got, err := ParseStat(makeProc(), 1000)
	if err != nil {
		t.Fatalf("ParseStat: %v", err)
	}
	// 2 CPUs × 10 modes + boot_time = 21 samples.
	if len(got) != 21 {
		t.Fatalf("got %d samples, want 21", len(got))
	}
	var bootTime float64
	cpu0User := -1.0
	cpu1Idle := -1.0
	for _, s := range got {
		switch s.Metric {
		case "node_boot_time_seconds":
			bootTime = s.Value
		case "node_cpu_seconds_total":
			if s.Labels["cpu"] == "0" && s.Labels["mode"] == "user" {
				cpu0User = s.Value
			}
			if s.Labels["cpu"] == "1" && s.Labels["mode"] == "idle" {
				cpu1Idle = s.Value
			}
		}
	}
	if bootTime != 1700000000 {
		t.Errorf("boot time = %v, want 1700000000", bootTime)
	}
	if cpu0User != 0.5 {
		t.Errorf("cpu0 user = %v, want 0.5 (50 jiffies / 100Hz)", cpu0User)
	}
	if cpu1Idle != 4.0 {
		t.Errorf("cpu1 idle = %v, want 4.0 (400 jiffies / 100Hz)", cpu1Idle)
	}
}

func TestParseMeminfoConvertsKBToBytes(t *testing.T) {
	got, err := ParseMeminfo(makeProc(), 1000)
	if err != nil {
		t.Fatalf("ParseMeminfo: %v", err)
	}
	idx := indexByMetricLabel(got, "", "")
	if idx["node_memory_MemTotal_bytes"] != 2048000*1024 {
		t.Errorf("MemTotal = %v, want %v", idx["node_memory_MemTotal_bytes"], 2048000*1024)
	}
	if idx["node_memory_MemAvailable_bytes"] != 1024000*1024 {
		t.Errorf("MemAvailable = %v, want %v", idx["node_memory_MemAvailable_bytes"], 1024000*1024)
	}
	if _, ok := idx["node_memory_Dirty_bytes"]; ok {
		t.Error("Dirty is not in the curated subset; should be skipped")
	}
}

func TestParseLoadavgEmitsThreeWindows(t *testing.T) {
	got, err := ParseLoadavg(makeProc(), 1000)
	if err != nil {
		t.Fatalf("ParseLoadavg: %v", err)
	}
	idx := indexByMetricLabel(got, "", "")
	if idx["node_load1"] != 0.42 {
		t.Errorf("load1 = %v", idx["node_load1"])
	}
	if idx["node_load5"] != 0.55 {
		t.Errorf("load5 = %v", idx["node_load5"])
	}
	if idx["node_load15"] != 0.61 {
		t.Errorf("load15 = %v", idx["node_load15"])
	}
}

func TestParseNetDevEmitsRxTxPerInterface(t *testing.T) {
	got, err := ParseNetDev(makeProc(), 1000)
	if err != nil {
		t.Fatalf("ParseNetDev: %v", err)
	}
	eth0 := indexByMetricLabel(got, "device", "eth0")
	if eth0["node_network_receive_bytes_total"] != 567890 {
		t.Errorf("eth0 rx_bytes = %v", eth0["node_network_receive_bytes_total"])
	}
	if eth0["node_network_transmit_bytes_total"] != 234567 {
		t.Errorf("eth0 tx_bytes = %v", eth0["node_network_transmit_bytes_total"])
	}
	if eth0["node_network_receive_errs_total"] != 1 {
		t.Errorf("eth0 rx_errs = %v", eth0["node_network_receive_errs_total"])
	}
	lo := indexByMetricLabel(got, "device", "lo")
	if lo["node_network_receive_bytes_total"] != 1234 {
		t.Errorf("lo rx_bytes = %v", lo["node_network_receive_bytes_total"])
	}
}

func TestParseDiskstatsSkipsLoopAndDM(t *testing.T) {
	got, err := ParseDiskstats(makeProc(), 1000)
	if err != nil {
		t.Fatalf("ParseDiskstats: %v", err)
	}
	devices := map[string]bool{}
	for _, s := range got {
		devices[s.Labels["device"]] = true
	}
	if !devices["sda"] || !devices["sdb"] {
		t.Errorf("expected sda+sdb in %v", devices)
	}
	if devices["loop0"] || devices["dm-0"] {
		t.Errorf("loop/dm devices should be filtered, got %v", devices)
	}
	sda := indexByMetricLabel(got, "device", "sda")
	// 2048 sectors * 512 = 1048576 bytes read.
	if sda["node_disk_read_bytes_total"] != 1048576 {
		t.Errorf("sda read_bytes = %v, want 1048576", sda["node_disk_read_bytes_total"])
	}
	if sda["node_disk_written_bytes_total"] != 4096*512 {
		t.Errorf("sda written_bytes = %v, want %v", sda["node_disk_written_bytes_total"], 4096*512)
	}
}
