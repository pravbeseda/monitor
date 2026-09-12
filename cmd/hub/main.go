// Command hub receives measurements, stores them and serves the web page.
package main

import (
	// A zone name in the configuration resolves only where a zone database is; linking one
	// in keeps digest.timezone working on a host that ships none.
	_ "time/tzdata"

	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pravbeseda/monitor/internal/config"
	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/notify"
	"github.com/pravbeseda/monitor/internal/storage"
	"github.com/pravbeseda/monitor/internal/version"
)

// readHeaderTimeout keeps a stalled client from holding a connection open forever.
const readHeaderTimeout = 10 * time.Second

// shutdownTimeout bounds how long a stop waits for requests in flight.
const shutdownTimeout = 10 * time.Second

// defaultListen keeps the hub on loopback: it is reached through a reverse proxy
// (docs/nginx-requirements.md), never directly.
const defaultListen = "127.0.0.1:8080"

// listenEnv names the address in the hub's environment file, which is where a per-host
// setting belongs — the unit that starts the service stays a constant (ADR 0019).
const listenEnv = "MONITOR_LISTEN"

// errVersionRequested is --version answered on stdout: a request, like -h, not a failure.
var errVersionRequested = errors.New("version requested")

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		// -h and --version have already printed their answer; both are requests.
		if errors.Is(err, flag.ErrHelp) || errors.Is(err, errVersionRequested) {
			return
		}
		fmt.Fprintf(os.Stderr, "hub: %v\n", err)
		os.Exit(1)
	}
}

// options are the deployment settings the hub needs; only the listen address has a
// product default, because binding to localhost says nothing about an installation.
type options struct {
	config string
	db     string
	listen string
}

func run(args []string, out io.Writer) error {
	opts, err := parseFlags(args, out)
	if err != nil {
		return err
	}
	cfg, err := config.Load(opts.config)
	if err != nil {
		return err
	}
	store, err := storage.OpenSQLite(opts.db)
	if err != nil {
		return err
	}
	defer func() {
		if err := store.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "hub: %v\n", err)
		}
	}()

	channel, err := notify.New(cfg.Notify())
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(out, "monitor-hub %s listening on %s (nodes: %d, notify: %s)\n",
		version.Current, opts.listen, len(cfg.Nodes()), cfg.Notify().Channel); err != nil {
		return fmt.Errorf("write to stdout: %w", err)
	}

	// A signal cancels the context, which stops the evaluation pass and the server
	// together: a change already recorded stays recorded, an in-flight send is abandoned.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	server := &http.Server{
		Addr:              opts.listen,
		Handler:           hub.Routes(cfg, store, time.Now),
		ReadHeaderTimeout: readHeaderTimeout,
	}
	evaluating := make(chan struct{})
	serving := make(chan struct{})

	// Both goroutines write to the database that the deferred Close takes away, so
	// whatever ends the run — a signal or a listener that never came up — cancels them
	// first and waits: a delivery recorded after the handle went would be sent again.
	defer func() {
		stop()
		<-evaluating
		<-serving
	}()

	go func() {
		defer close(evaluating)
		evaluate.New(evaluate.Options{
			Store:    store,
			Notifier: channel,
			Targets:  cfg.Targets(),
			Digest:   cfg.Digest(),
			Started:  time.Now(),
			Now:      time.Now,
		}).Run(ctx, evaluate.Interval)
	}()
	go func() {
		defer close(serving)
		<-ctx.Done()
		// Shutdown closes the listeners at once and then drains what is in flight, so
		// ListenAndServe returns long before this does.
		closing, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(closing); err != nil {
			fmt.Fprintf(os.Stderr, "hub: stop serving: %v\n", err)
		}
	}()

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve on %s: %w", opts.listen, err)
	}
	return nil
}

// listenAddress is the environment's address, or the product default. An empty variable is
// a variable nobody set: systemd keeps a bare `MONITOR_LISTEN=` line, and an empty address
// would serve port 80 on every interface.
func listenAddress() string {
	if addr := os.Getenv(listenEnv); addr != "" {
		return addr
	}
	return defaultListen
}

// parseFlags reads the deployment paths. Its own errors are printed by the caller, so the
// flag package stays quiet — except for -h, which is answered with the flag list on out.
func parseFlags(args []string, out io.Writer) (options, error) {
	flags := flag.NewFlagSet("hub", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var opts options
	var showVersion bool
	flags.BoolVar(&showVersion, "version", false, "print the version and exit")
	flags.StringVar(&opts.config, "config", "", "path to the hub's YAML configuration")
	flags.StringVar(&opts.db, "db", "", "path to the SQLite database")
	flags.StringVar(&opts.listen, "listen", listenAddress(), "address to serve on ("+listenEnv+" sets it for a service)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(out)
			flags.Usage()
			return options{}, err
		}
		return options{}, fmt.Errorf("parse flags: %w", err)
	}
	// Answered before anything is required, so a freshly downloaded binary can be asked
	// which version it is (docs/specs/release.md).
	if showVersion {
		if _, err := fmt.Fprintf(out, "monitor-hub %s\n", version.Current); err != nil {
			return options{}, fmt.Errorf("write to stdout: %w", err)
		}
		return options{}, errVersionRequested
	}
	if opts.config == "" {
		return options{}, errors.New("--config is required: the configuration path has no default")
	}
	if opts.db == "" {
		return options{}, errors.New("--db is required: the database path has no default")
	}
	if err := requireLoopback(opts.listen); err != nil {
		return options{}, err
	}
	return opts, nil
}

// requireLoopback refuses an address that faces the network. What guards the pages and the
// read API is the reverse proxy in front of the hub (ADR 0023), so a hub reachable on a
// public interface serves both to anyone who finds the port.
func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen address %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("listen address %q is not loopback: the hub is reached through a reverse proxy, "+
		"which is what authenticates its pages", addr)
}
