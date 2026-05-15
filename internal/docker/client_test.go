package docker

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

// startUnixSocketServer spins up an HTTP server bound to a Unix socket
// inside t.TempDir, with the given handler. Returns the socket path.
func startUnixSocketServer(t *testing.T, h http.Handler) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "docker.sock")
	lis, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return sock
}

func TestListContainers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"Id":"abc","Names":["/owl"],"Image":"owl:dev","State":"running","Labels":{"owl.scrape":"true"}},
			{"Id":"def","Names":["/traefik"],"Image":"traefik:v3","State":"running","Labels":{}}
		]`))
	})
	sock := startUnixSocketServer(t, mux)

	c := NewClient(sock)
	cts, err := c.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(cts) != 2 {
		t.Fatalf("got %d, want 2", len(cts))
	}
	if cts[0].Name() != "owl" {
		t.Errorf("Name = %q, want owl", cts[0].Name())
	}
	if cts[0].Labels["owl.scrape"] != "true" {
		t.Errorf("missing label owl.scrape=true: %+v", cts[0].Labels)
	}
}

func TestContainerStats(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/abc/stats", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"read": "2025-01-01T00:00:00Z",
			"cpu_stats": {"cpu_usage": {"total_usage": 200000000}, "system_cpu_usage": 1000000000, "online_cpus": 2},
			"precpu_stats": {"cpu_usage": {"total_usage": 100000000}, "system_cpu_usage": 900000000, "online_cpus": 2},
			"memory_stats": {"usage": 1048576, "max_usage": 2097152, "limit": 4194304},
			"networks": {"eth0": {"rx_bytes": 1234, "tx_bytes": 5678}},
			"blkio_stats": {"io_service_bytes_recursive": [
				{"op": "Read", "value": 4096},
				{"op": "Write", "value": 8192}
			]}
		}`))
	})
	sock := startUnixSocketServer(t, mux)

	c := NewClient(sock)
	st, err := c.ContainerStats(context.Background(), "abc")
	if err != nil {
		t.Fatalf("ContainerStats: %v", err)
	}
	if st.CPUStats.CPUUsage.TotalUsage != 200_000_000 {
		t.Errorf("cpu total = %d", st.CPUStats.CPUUsage.TotalUsage)
	}
	if st.Memory.Usage != 1_048_576 {
		t.Errorf("memory usage = %d", st.Memory.Usage)
	}
	if st.Networks["eth0"].RxBytes != 1234 {
		t.Errorf("net rx = %d", st.Networks["eth0"].RxBytes)
	}
}

func TestNon200StatusReturnsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
	sock := startUnixSocketServer(t, mux)

	c := NewClient(sock)
	_, err := c.ListContainers(context.Background())
	if err == nil {
		t.Fatal("expected error on 401")
	}
}
