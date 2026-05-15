package config

import (
	"testing"
	"time"
)

func TestDefaultConfigInvariants(t *testing.T) {
	c := Default()

	if c.Listen == "" {
		t.Error("Listen must have a default")
	}
	if c.Storage.Path == "" {
		t.Error("Storage.Path must have a default")
	}
	if c.Storage.Retention.Time <= 0 {
		t.Error("Storage.Retention.Time must default to a positive duration")
	}
	if c.Scrape.DefaultInterval <= 0 {
		t.Error("Scrape.DefaultInterval must default to a positive duration")
	}
	if c.Scrape.DefaultTimeout <= 0 || c.Scrape.DefaultTimeout >= c.Scrape.DefaultInterval {
		t.Errorf("Scrape.DefaultTimeout (%v) must be positive and < DefaultInterval (%v)",
			c.Scrape.DefaultTimeout, c.Scrape.DefaultInterval)
	}
	if c.Discovery.Docker.SocketPath == "" {
		t.Error("Discovery.Docker.SocketPath must have a default")
	}
	if c.Discovery.Docker.LabelPrefix == "" {
		t.Error("Discovery.Docker.LabelPrefix must have a default")
	}
	if c.Dashboards.Dir == "" {
		t.Error("Dashboards.Dir must have a default")
	}
	if c.LogLevel == "" {
		t.Error("LogLevel must have a default")
	}

	_ = time.Second // keep import lively in case retention check shifts
}
