package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/neverbot/owl/internal/alert"
	"github.com/neverbot/owl/internal/collect/host"
	"github.com/neverbot/owl/internal/config"
	"github.com/neverbot/owl/internal/dashboards"
	"github.com/neverbot/owl/internal/docker"
	"github.com/neverbot/owl/internal/events"
	"github.com/neverbot/owl/internal/events/drivers"
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
		Interval: cfg.Storage.Retention.Interval,
	}
	spawn(func() { retention.Run(ctx) })

	// Chunk flusher: periodically compresses head samples older than
	// the configured head window into Gorilla-encoded chunks. This is
	// what turns the per-sample disk footprint from ~30 bytes (raw)
	// down to ~3-5 bytes (compressed). See
	// `context/decisions/2026-06-07-storage-footprint.md` for the
	// full reasoning.
	flusher := &storage.Flusher{
		Store:      store,
		HeadWindow: cfg.Storage.HeadWindow,
		Interval:   cfg.Storage.FlushInterval,
	}
	spawn(func() { flusher.Run(ctx) })
	slog.Info("chunk flusher started",
		"head_window", cfg.Storage.HeadWindow,
		"flush_interval", cfg.Storage.FlushInterval)

	// Optional Linux host collector (/proc + /sys). Disabled by default
	// because /proc does not exist on every platform owl might run on
	// (macOS dev environments, distroless without bind-mounts, etc.).
	// Kept in scope so the web layer can surface its HealthSnapshot on
	// /targets alongside HTTP scrape targets.
	var hostCol *host.Collector
	if cfg.Host.Enabled {
		hostCol = host.New(store, host.Options{
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
	// dockerCol and dockerDisc stay in outer scope so the web layer can
	// surface their HealthSnapshots on /targets when each integration
	// is enabled.
	var dockerCol *docker.Collector
	var dockerDisc *docker.Discovery
	if cfg.Docker.Enabled {
		dockerClient := docker.NewClient(cfg.Docker.SocketPath)

		if cfg.Docker.Metrics.Enabled {
			dockerCol = docker.NewCollector(dockerClient, store, cfg.Docker.Metrics.Interval)
			spawn(func() { dockerCol.Run(ctx) })
			slog.Info("docker metrics collector started",
				"socket", cfg.Docker.SocketPath, "interval", cfg.Docker.Metrics.Interval)
		}

		if cfg.Docker.Discovery.Enabled {
			dockerDisc = docker.NewDiscovery(dockerClient, docker.DiscoveryOptions{
				Prefix:          cfg.Docker.Discovery.LabelPrefix,
				Interval:        cfg.Docker.Discovery.Interval,
				DefaultInterval: cfg.Scrape.DefaultInterval,
				DefaultTimeout:  cfg.Scrape.DefaultTimeout,
			})
			snapshots := make(chan []scrape.Target, 4)
			spawn(func() { dockerDisc.Run(ctx, snapshots) })
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

	// Events manager: one goroutine per source, hot-reloadable like
	// scrape targets. Disabled by default — opt in with events.enabled.
	var eventsMgr *events.Manager
	buildEventSources := func(c config.Config, dockerCli *docker.Client) ([]events.Source, error) {
		out := make([]events.Source, 0, len(c.Events.Sources))
		for _, src := range c.Events.Sources {
			var drv events.Driver
			switch src.Driver {
			case "file_tail":
				drv = drivers.NewFileTail(src.Name, src.Path, src.From)
			case "docker_logs":
				if dockerCli == nil {
					return nil, fmt.Errorf("events source %q: docker_logs requires docker.enabled", src.Name)
				}
				drv = drivers.NewDockerLogs(src.Name, src.Container, dockerLogsAdapter{dockerCli})
			default:
				return nil, fmt.Errorf("events source %q: unknown driver %q", src.Name, src.Driver)
			}
			var re *regexp.Regexp
			if src.Format == "regex" {
				re = regexp.MustCompile(src.Pattern)
			}
			tpl, err := events.CompileTemplate(src.Name, src.Render)
			if err != nil {
				return nil, err
			}
			out = append(out, events.Source{
				Name:     src.Name,
				Driver:   drv,
				Interval: src.Interval,
				Format:   src.Format,
				Pattern:  re,
				Match:    src.Match,
				Mapping:  src.Mapping,
				Template: tpl,
			})
		}
		return out, nil
	}
	if cfg.Events.Enabled {
		eventsStore := events.NewStore(store.DB())
		eventsMgr = events.NewManager(eventsStore)
		var dcli *docker.Client
		if cfg.Docker.Enabled {
			dcli = docker.NewClient(cfg.Docker.SocketPath)
		}
		srcs, err := buildEventSources(cfg, dcli)
		if err != nil {
			return fmt.Errorf("events: %w", err)
		}
		eventsMgr.SetSources(srcs)
		spawn(func() { eventsMgr.Run(ctx) })
		slog.Info("events manager started", "sources", len(srcs))
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
		if eventsMgr != nil {
			var dcli *docker.Client
			if newCfg.Docker.Enabled {
				dcli = docker.NewClient(newCfg.Docker.SocketPath)
			}
			srcs, err := buildEventSources(newCfg, dcli)
			if err != nil {
				return fmt.Errorf("events: %w", err)
			}
			eventsMgr.SetSources(srcs)
		}
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
			Store:      store,
			Engine:     engine,
			Loader:     dashLoader,
			Scrape:     scrapeMgr,
			Collectors: collectorsAdapter{host: hostCol, docker: dockerCol, discovery: dockerDisc},
			Containers: containersAdapter{docker: dockerCol},
			Alerter:    alerter,
			OnReload:   reload,
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

// buildTargets translates the YAML target list into runtime
// scrape.Target objects. Regex filters are compiled here; config
// validation already rejected invalid patterns at startup, so a
// surprise compile error this late means an internal bug.
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
		st := scrape.Target{
			Name:     t.Name,
			URL:      t.URL,
			Interval: interval,
			Timeout:  timeout,
			Labels:   labels,
			Keep:     mustCompilePatterns(t.Keep),
			Drop:     mustCompilePatterns(t.Drop),
		}
		if t.Auth != nil {
			st.BearerToken = t.Auth.BearerToken
			if t.Auth.Basic != nil {
				st.BasicAuth = &scrape.BasicAuth{
					Username: t.Auth.Basic.Username,
					Password: t.Auth.Basic.Password,
				}
			}
			if len(t.Auth.Headers) > 0 {
				st.Headers = make(map[string]string, len(t.Auth.Headers))
				for k, v := range t.Auth.Headers {
					st.Headers[k] = v
				}
			}
		}
		if t.TLS != nil {
			st.TLS = &scrape.TLSOptions{
				InsecureSkipVerify: t.TLS.InsecureSkipVerify,
				CAFile:             t.TLS.CAFile,
			}
		}
		out = append(out, st)
	}
	return out
}

// mustCompilePatterns compiles every pattern in the slice or panics.
// Patterns are validated up-front by config.Validate; reaching here
// with an invalid pattern would mean a code path skipped validation.
func mustCompilePatterns(patterns []string) []*regexp.Regexp {
	if len(patterns) == 0 {
		return nil
	}
	out := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		out[i] = regexp.MustCompile(p)
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

// collectorsAdapter implements web.CollectorsHealth by translating
// each enabled in-process collector's native Health type into the
// uniform web.CollectorHealth shape rendered on /targets. Fields are
// nil when the corresponding collector is disabled, in which case it
// is skipped.
type collectorsAdapter struct {
	host      *host.Collector
	docker    *docker.Collector
	discovery *docker.Discovery
}

// CollectorsSnapshot returns one entry per enabled collector. Order is
// stable: host, docker metrics, docker discovery. The web layer is
// free to call this on every render — all underlying snapshots are
// cheap copies guarded by RWMutex.
func (a collectorsAdapter) CollectorsSnapshot() []web.CollectorHealth {
	var out []web.CollectorHealth
	if a.host != nil {
		h := a.host.HealthSnapshot()
		out = append(out, web.CollectorHealth{
			Name:           "host",
			Kind:           "host",
			Interval:       h.Interval,
			LastCollection: h.LastCollection,
			Duration:       h.Duration,
			LastError:      h.LastError,
			LastSamples:    h.LastSamples,
		})
	}
	if a.docker != nil {
		h := a.docker.HealthSnapshot()
		extra := ""
		if h.ContainersSeen > 0 {
			suffix := "s"
			if h.ContainersSeen == 1 {
				suffix = ""
			}
			extra = fmt.Sprintf("%d container%s seen", h.ContainersSeen, suffix)
		}
		out = append(out, web.CollectorHealth{
			Name:           "docker",
			Kind:           "docker_metrics",
			Interval:       h.Interval,
			LastCollection: h.LastCollection,
			Duration:       h.Duration,
			LastError:      h.LastError,
			LastSamples:    h.LastSamples,
			Extra:          extra,
		})
	}
	if a.discovery != nil {
		h := a.discovery.HealthSnapshot()
		extra := ""
		if h.LastError == "" && !h.LastScan.IsZero() {
			extra = fmt.Sprintf("%d of %d containers opted in", h.OptedIn, h.ContainersSeen)
		}
		out = append(out, web.CollectorHealth{
			Name:           "docker-discovery",
			Kind:           "docker_discovery",
			Interval:       h.Interval,
			LastCollection: h.LastScan,
			Duration:       h.Duration,
			LastError:      h.LastError,
			LastSamples:    h.OptedIn,
			Extra:          extra,
		})
	}
	return out
}

// containersAdapter implements web.ContainersHealth by reading the
// docker collector's per-container snapshot and translating it to the
// uniform web.ContainerInfo shape rendered on /targets. Returns nil
// when the docker integration is disabled so the section is omitted
// from both the page and the JSON.
type containersAdapter struct {
	docker *docker.Collector
}

// ContainersSnapshot returns one entry per container observed on the
// docker collector's last successful tick. Order matches the
// collector's own sort (by name).
func (a containersAdapter) ContainersSnapshot() []web.ContainerInfo {
	if a.docker == nil {
		return nil
	}
	snap := a.docker.ContainersSnapshot()
	if len(snap) == 0 {
		return nil
	}
	out := make([]web.ContainerInfo, 0, len(snap))
	for _, c := range snap {
		out = append(out, web.ContainerInfo{
			Name:                  c.Name,
			Image:                 c.Image,
			ComposeService:        c.ComposeService,
			ComposeProject:        c.ComposeProject,
			MemoryWorkingSetBytes: c.MemoryWorkingSet,
			LastSeen:              c.LastSeen,
		})
	}
	return out
}

// dockerLogsAdapter satisfies events/drivers.LogsClient against an
// internal/docker.Client without leaking the docker dependency into
// the events package.
type dockerLogsAdapter struct{ c *docker.Client }

// ContainerLogs fetches stdout+stderr for one container, optionally
// filtered by since (RFC3339Nano), with embedded timestamps so the
// driver can parse the per-line stamp into Record.RawTS.
func (a dockerLogsAdapter) ContainerLogs(ctx context.Context, container, since string) (io.ReadCloser, error) {
	return a.c.ContainerLogs(ctx, container, since)
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
