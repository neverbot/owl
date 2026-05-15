// Package docker is owl's minimal Docker Engine API client. It talks
// to the daemon over a Unix socket using plain net/http, avoiding the
// full Docker SDK (which would add tens of megabytes of dependencies).
//
// Two endpoints are exercised:
//
//	GET /containers/json              — list running containers
//	GET /containers/{id}/stats?stream=false — one snapshot of stats
//
// That's enough surface to feed both the container metrics collector
// and the label-based scrape discovery.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Client is a tiny Docker daemon client.
type Client struct {
	httpClient *http.Client
	// host is the host name we put in the URL. It is irrelevant when
	// dialing a Unix socket but http.Request requires a Host header.
	host string
}

// NewClient returns a Client that talks to the daemon at socketPath
// (typically /var/run/docker.sock).
func NewClient(socketPath string) *Client {
	return &Client{
		host: "docker",
		httpClient: &http.Client{
			Timeout: 8 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socketPath)
				},
			},
		},
	}
}

// Container is the subset of `/containers/json` we care about.
type Container struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
}

// Name returns the primary name without the leading slash. Docker
// returns names like "/owl" so we strip it for use in labels.
func (c Container) Name() string {
	for _, n := range c.Names {
		if len(n) > 0 && n[0] == '/' {
			return n[1:]
		}
		if n != "" {
			return n
		}
	}
	return c.ID[:12]
}

// ListContainers returns the running containers on the daemon.
func (c *Client) ListContainers(ctx context.Context) ([]Container, error) {
	var out []Container
	if err := c.get(ctx, "/containers/json", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Stats is the subset of /containers/{id}/stats we use. The Docker API
// returns a much larger JSON; we only decode the fields we need.
type Stats struct {
	Read     time.Time      `json:"read"`
	CPUStats CPUStats       `json:"cpu_stats"`
	PreCPU   CPUStats       `json:"precpu_stats"`
	Memory   MemoryStats    `json:"memory_stats"`
	Networks map[string]Net `json:"networks"`
	BlkIO    BlockIO        `json:"blkio_stats"`
}

type CPUStats struct {
	CPUUsage struct {
		TotalUsage  uint64   `json:"total_usage"`
		PercpuUsage []uint64 `json:"percpu_usage"`
	} `json:"cpu_usage"`
	SystemCPUUsage uint64 `json:"system_cpu_usage"`
	OnlineCPUs     uint64 `json:"online_cpus"`
}

type MemoryStats struct {
	Usage    uint64            `json:"usage"`
	MaxUsage uint64            `json:"max_usage"`
	Limit    uint64            `json:"limit"`
	Stats    map[string]uint64 `json:"stats"`
}

type Net struct {
	RxBytes   uint64 `json:"rx_bytes"`
	RxPackets uint64 `json:"rx_packets"`
	RxErrors  uint64 `json:"rx_errors"`
	TxBytes   uint64 `json:"tx_bytes"`
	TxPackets uint64 `json:"tx_packets"`
	TxErrors  uint64 `json:"tx_errors"`
}

type BlockIO struct {
	IoServiceBytesRecursive []BlockIOEntry `json:"io_service_bytes_recursive"`
}

type BlockIOEntry struct {
	Major uint64 `json:"major"`
	Minor uint64 `json:"minor"`
	Op    string `json:"op"` // "Read", "Write", "Total", …
	Value uint64 `json:"value"`
}

// ContainerStats fetches one snapshot of stats for the given container.
// Uses ?stream=false so the daemon returns immediately instead of
// holding the connection open.
func (c *Client) ContainerStats(ctx context.Context, id string) (*Stats, error) {
	var out Stats
	if err := c.get(ctx, "/containers/"+id+"/stats?stream=false&one-shot=true", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// get issues a GET request to the daemon and decodes the JSON body.
func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+c.host+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("docker %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("docker %s: status %d: %s", path, resp.StatusCode, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
