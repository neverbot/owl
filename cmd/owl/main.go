package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/neverbot/owl/internal/alert"
	"github.com/neverbot/owl/internal/collect/host"
	rtcollect "github.com/neverbot/owl/internal/collect/runtime"
	"github.com/neverbot/owl/internal/config"
	"github.com/neverbot/owl/internal/dashboards"
	"github.com/neverbot/owl/internal/docker"
	"github.com/neverbot/owl/internal/query"
	"github.com/neverbot/owl/internal/scrape"
	"github.com/neverbot/owl/internal/storage"
	"github.com/neverbot/owl/internal/version"
	"github.com/neverbot/owl/internal/web"
)

func main() {
	var (
		configPath  string
		showVersion bool
		checkOnly   bool
	)
	flag.StringVar(&configPath, "config", "/etc/owl/config.yml", "path to the config file")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.BoolVar(&checkOnly, "check-config", false, "validate the config and exit")
	flag.Parse()

	if showVersion {
		fmt.Println(version.String())
		return
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fail(err)
	}
	config.ApplyEnv(&cfg)
	if err := config.Validate(&cfg); err != nil {
		fail(err)
	}
	if checkOnly {
		fmt.Fprintf(os.Stderr, "owl: config %s OK\n", configPath)
		return
	}

	if err := run(cfg); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "owl: %v\n", err)
	os.Exit(2)
}

func run(cfg config.Config) error {
	if err := ensureDir(cfg.Storage.Path); err != nil {
		return fmt.Errorf("storage dir: %w", err)
	}
	store, err := storage.Open(cfg.Storage.Path)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	retention := &storage.Worker{
		Store:    store,
		Time:     cfg.Storage.Retention.Time,
		Size:     cfg.Storage.Retention.Size,
		Interval: 5 * time.Minute,
	}
	go retention.Run(ctx)

	collector := rtcollect.New(store, cfg.Scrape.DefaultInterval)
	go collector.Run(ctx)

	// Optional Linux host collector (/proc + /sys). Disabled by default
	// because /proc does not exist on every platform owl might run on
	// (macOS dev environments, distroless without bind-mounts, etc.).
	if cfg.Host.Enabled {
		hostCol := host.New(store, host.Options{
			ProcPath: cfg.Host.ProcPath,
			Interval: cfg.Host.Interval,
		})
		go hostCol.Run(ctx)
		fmt.Fprintf(os.Stderr, "owl: host collector reading %s every %s\n",
			cfg.Host.ProcPath, cfg.Host.Interval)
	}

	// HTTP scrape manager.
	scrapeMgr := scrape.NewManager(store)
	yamlTargets := buildTargets(cfg)
	scrapeMgr.Set(yamlTargets)
	go scrapeMgr.Run(ctx)

	// Optional Docker integration: per-container metrics and / or
	// label-based scrape-target discovery. Both share one HTTP-over-
	// Unix-socket client. Disabled by default so the binary works on
	// hosts without a Docker daemon.
	if cfg.Docker.Enabled {
		dockerClient := docker.NewClient(cfg.Docker.SocketPath)

		if cfg.Docker.Metrics.Enabled {
			dockerCol := docker.NewCollector(dockerClient, store, cfg.Docker.Metrics.Interval)
			go dockerCol.Run(ctx)
			fmt.Fprintf(os.Stderr, "owl: docker metrics collector reading %s every %s\n",
				cfg.Docker.SocketPath, cfg.Docker.Metrics.Interval)
		}

		if cfg.Docker.Discovery.Enabled {
			disc := docker.NewDiscovery(dockerClient, docker.DiscoveryOptions{
				Prefix:          cfg.Docker.Discovery.LabelPrefix,
				Interval:        cfg.Docker.Discovery.Interval,
				DefaultInterval: cfg.Scrape.DefaultInterval,
				DefaultTimeout:  cfg.Scrape.DefaultTimeout,
			})
			snapshots := make(chan []scrape.Target, 4)
			go disc.Run(ctx, snapshots)
			go func() {
				for found := range snapshots {
					merged := make([]scrape.Target, 0, len(yamlTargets)+len(found))
					merged = append(merged, yamlTargets...)
					merged = append(merged, found...)
					scrapeMgr.Set(merged)
				}
			}()
			fmt.Fprintf(os.Stderr, "owl: docker discovery scanning for label %q every %s\n",
				cfg.Docker.Discovery.LabelPrefix, cfg.Docker.Discovery.Interval)
		}
	}

	// Query engine.
	engine := query.NewEngine(store)

	// Optional alerter. Threshold rules from cfg.Alerts.Rules are
	// evaluated against the query engine; transitions are POSTed to
	// the configured webhook. Disabled when no rules are configured.
	if len(cfg.Alerts.Rules) > 0 {
		var rules []alert.Rule
		for _, r := range cfg.Alerts.Rules {
			rules = append(rules, alert.Rule{
				Name:      r.Name,
				Expr:      r.Expr,
				Op:        r.Op,
				Threshold: r.Threshold,
				For:       r.For,
			})
		}
		var wh alert.Webhook
		if cfg.Alerts.WebhookURL != "" {
			wh = alert.NewHTTPWebhook(cfg.Alerts.WebhookURL)
		}
		alerter := alert.New(engine, wh, rules, 0)
		go alerter.Run(ctx)
		fmt.Fprintf(os.Stderr, "owl: alerter evaluating %d rule(s)\n", len(rules))
	}

	// Dashboard loader.
	dashLoader := dashboards.NewLoader(cfg.Dashboards.Dir, engine)
	if err := dashLoader.Reload(); err != nil {
		return fmt.Errorf("dashboards: %w", err)
	}

	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: web.NewServer(web.Options{
			Store:  store,
			Engine: engine,
			Loader: dashLoader,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	httpErr := make(chan error, 1)
	go func() {
		fmt.Fprintf(os.Stderr, "owl: %s\n", describeListen(cfg.Listen))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "owl: shutting down")
	case err := <-httpErr:
		cancel()
		return err
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	return srv.Shutdown(shutdownCtx)
}

func buildTargets(cfg config.Config) []scrape.Target {
	defInterval := cfg.Scrape.DefaultInterval
	defTimeout := cfg.Scrape.DefaultTimeout
	out := make([]scrape.Target, 0, len(cfg.Targets))
	for _, t := range cfg.Targets {
		interval := t.Interval
		if interval <= 0 {
			interval = defInterval
		}
		timeout := t.Timeout
		if timeout <= 0 {
			timeout = defTimeout
		}
		labels := map[string]string{}
		for k, v := range t.Labels {
			labels[k] = v
		}
		out = append(out, scrape.Target{
			Name:     t.Name,
			URL:      t.URL,
			Interval: interval,
			Timeout:  timeout,
			Labels:   labels,
		})
	}
	return out
}

// describeListen turns a bind address into a human-readable startup
// message. When the bind is to a wildcard address (0.0.0.0, ::, or an
// empty host), the address is not a clickable URL from outside the
// process's own network namespace — printing it as one (especially
// inside a container, where it's the default and most users land here)
// is misleading. Specific binds (loopback, host IP, VPN IP) DO produce
// a URL the operator can paste.
func describeListen(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "listening on " + addr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return "listening on port " + port + " (all interfaces)"
	default:
		return "listening on http://" + addr
	}
}

func ensureDir(path string) error {
	dir := path
	for i := len(dir) - 1; i >= 0; i-- {
		if dir[i] == '/' {
			dir = dir[:i]
			break
		}
	}
	if dir == "" || dir == path {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}
