package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
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

	configureLogger(cfg.LogLevel)
	if err := run(cfg, configPath); err != nil {
		fail(err)
	}
}

// configureLogger installs a structured-logging default. The text
// handler keeps output readable in `docker logs` while still producing
// key=value pairs that downstream log shippers can parse. Level
// follows config (info, warn, debug, error); unknown values fall back
// to info.
func configureLogger(level string) {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn", "warning":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l})))
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "owl: %v\n", err)
	os.Exit(2)
}

func run(cfg config.Config, configPath string) error {
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

	// wg tracks every long-running goroutine started below so we can
	// wait for them on shutdown instead of letting the process exit
	// while a writer is still mid-batch. `spawn` is the canonical
	// idiom — `spawn(func() { x.Run(ctx) })` — and is used
	// throughout `run`.
	var wg sync.WaitGroup
	spawn := func(fn func()) {
		wg.Add(1)
		go func() { defer wg.Done(); fn() }()
	}

	retention := &storage.Worker{
		Store:    store,
		Time:     cfg.Storage.Retention.Time,
		Size:     cfg.Storage.Retention.Size,
		Interval: 5 * time.Minute,
	}
	spawn(func() { retention.Run(ctx) })

	collector := rtcollect.New(store, cfg.Scrape.DefaultInterval)
	spawn(func() { collector.Run(ctx) })

	// Optional Linux host collector (/proc + /sys). Disabled by default
	// because /proc does not exist on every platform owl might run on
	// (macOS dev environments, distroless without bind-mounts, etc.).
	if cfg.Host.Enabled {
		hostCol := host.New(store, host.Options{
			ProcPath: cfg.Host.ProcPath,
			Interval: cfg.Host.Interval,
		})
		spawn(func() { hostCol.Run(ctx) })
		slog.Info("host collector started", "proc_path", cfg.Host.ProcPath, "interval", cfg.Host.Interval)
	}

	// Query engine (constructed early so both the alerter and the
	// web layer share the same instance).
	engine := query.NewEngine(store)

	// HTTP scrape manager.
	scrapeMgr := scrape.NewManager(store)
	// yamlTargetsPtr lives behind an atomic.Pointer so the reload
	// closure (further down) and the discovery merge goroutine can
	// observe new target lists without locking.
	initial := buildTargets(cfg)
	var yamlTargetsPtr atomic.Pointer[[]scrape.Target]
	yamlTargetsPtr.Store(&initial)
	scrapeMgr.Set(initial)
	spawn(func() { scrapeMgr.Run(ctx) })

	// Optional Docker integration: per-container metrics and / or
	// label-based scrape-target discovery. Both share one HTTP-over-
	// Unix-socket client. Disabled by default so the binary works on
	// hosts without a Docker daemon.
	if cfg.Docker.Enabled {
		dockerClient := docker.NewClient(cfg.Docker.SocketPath)

		if cfg.Docker.Metrics.Enabled {
			dockerCol := docker.NewCollector(dockerClient, store, cfg.Docker.Metrics.Interval)
			spawn(func() { dockerCol.Run(ctx) })
			slog.Info("docker metrics collector started",
				"socket", cfg.Docker.SocketPath, "interval", cfg.Docker.Metrics.Interval)
		}

		if cfg.Docker.Discovery.Enabled {
			disc := docker.NewDiscovery(dockerClient, docker.DiscoveryOptions{
				Prefix:          cfg.Docker.Discovery.LabelPrefix,
				Interval:        cfg.Docker.Discovery.Interval,
				DefaultInterval: cfg.Scrape.DefaultInterval,
				DefaultTimeout:  cfg.Scrape.DefaultTimeout,
			})
			snapshots := make(chan []scrape.Target, 4)
			spawn(func() { disc.Run(ctx, snapshots) })
			spawn(func() {
				for found := range snapshots {
					y := *yamlTargetsPtr.Load()
					merged := make([]scrape.Target, 0, len(y)+len(found))
					merged = append(merged, y...)
					merged = append(merged, found...)
					scrapeMgr.Set(merged)
				}
			})
			slog.Info("docker discovery started",
				"label", cfg.Docker.Discovery.LabelPrefix, "interval", cfg.Docker.Discovery.Interval)
		}
	}

	// Alerter — always created so reload can populate / depopulate
	// its rule set without a restart. Disabled in effect when there
	// are no rules (the evaluation loop is a no-op).
	var webhook alert.Webhook
	if cfg.Alerts.WebhookURL != "" {
		webhook = alert.NewHTTPWebhook(cfg.Alerts.WebhookURL)
	}
	alerter := alert.New(engine, webhook, buildAlertRules(cfg), 0)
	spawn(func() { alerter.Run(ctx) })
	if n := len(cfg.Alerts.Rules); n > 0 {
		slog.Info("alerter started", "rules", n)
	}

	// Dashboard loader.
	dashLoader := dashboards.NewLoader(cfg.Dashboards.Dir, engine)
	if err := dashLoader.Reload(); err != nil {
		return fmt.Errorf("dashboards: %w", err)
	}

	// reload re-reads config.yml and dashboards/*.json atomically and
	// applies updates to live subsystems: scrape targets, alert rules,
	// and the in-memory dashboard index. Wired to both SIGHUP and
	// POST /-/reload. The webhook URL and the listener address still
	// require a process restart — they're set once when the HTTP
	// server is constructed.
	reload := func() error {
		newCfg, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
		config.ApplyEnv(&newCfg)
		if err := config.Validate(&newCfg); err != nil {
			return fmt.Errorf("config invalid: %w", err)
		}
		if err := dashLoader.Reload(); err != nil {
			return fmt.Errorf("dashboards: %w", err)
		}
		// Update the live scrape target list. The discovery merge
		// goroutine (if running) will pick up the new yamlTargets on
		// its next snapshot; we also push an immediate Set so the
		// reload feels instant when discovery is off or on a long
		// cadence.
		newYaml := buildTargets(newCfg)
		yamlTargetsPtr.Store(&newYaml)
		scrapeMgr.Set(newYaml)
		alerter.SetRules(buildAlertRules(newCfg))
		var newWebhook alert.Webhook
		if newCfg.Alerts.WebhookURL != "" {
			newWebhook = alert.NewHTTPWebhook(newCfg.Alerts.WebhookURL)
		}
		alerter.SetWebhook(newWebhook)
		slog.Info("reloaded",
			"targets", len(newYaml),
			"rules", len(newCfg.Alerts.Rules),
			"dashboards", len(dashLoader.List()))
		return nil
	}

	// Optional dashboards watcher. Polls *.json mtimes in
	// `dashboards.dir` and triggers a reload when any change. Opt-in:
	// owl does not poll your filesystem unless you ask. Defaults to a
	// 5s interval if the user enables the watcher without setting one.
	if cfg.Dashboards.Watch {
		interval := cfg.Dashboards.WatchInterval
		if interval <= 0 {
			interval = 5 * time.Second
		}
		watcher := &dashboards.Watcher{
			Dir:      cfg.Dashboards.Dir,
			Interval: interval,
			OnChange: func() error {
				if err := dashLoader.Reload(); err != nil {
					return err
				}
				slog.Info("dashboards changed, reloaded", "count", len(dashLoader.List()))
				return nil
			},
		}
		spawn(func() { watcher.Run(ctx) })
		slog.Info("dashboards watcher started", "dir", cfg.Dashboards.Dir, "interval", interval)
	}

	// Wire SIGHUP to the same hook. SIGHUP is the conventional
	// "re-read your config" signal on Unix.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	spawn(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-hup:
				if err := reload(); err != nil {
					slog.Error("reload failed", "err", err)
				}
			}
		}
	})

	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: web.NewServer(web.Options{
			Store:    store,
			Engine:   engine,
			Loader:   dashLoader,
			Scrape:   scrapeMgr,
			OnReload: reload,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	httpErr := make(chan error, 1)
	spawn(func() {
		slog.Info(describeListen(cfg.Listen))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErr <- err
		}
	})

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
	case err := <-httpErr:
		cancel()
		return err
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		// Returning is fine; the goroutine wait below still happens.
		slog.Error("http shutdown", "err", err)
	}

	// Wait for every collector / worker / handler to exit. Use a
	// timeout: a stuck goroutine should not prevent the process from
	// terminating eventually.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		slog.Info("clean shutdown")
	case <-time.After(8 * time.Second):
		slog.Warn("shutdown timed out — some goroutines did not exit")
	}
	return nil
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

// buildAlertRules converts the YAML alert rules into the alert
// package's runtime representation.
func buildAlertRules(cfg config.Config) []alert.Rule {
	out := make([]alert.Rule, 0, len(cfg.Alerts.Rules))
	for _, r := range cfg.Alerts.Rules {
		out = append(out, alert.Rule{
			Name:      r.Name,
			Expr:      r.Expr,
			Op:        r.Op,
			Threshold: r.Threshold,
			For:       r.For,
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
