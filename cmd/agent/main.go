// Command agent collects measurements on a node and pushes them to the hub.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pravbeseda/monitor/internal/agent"
	"github.com/pravbeseda/monitor/internal/sensor"
	"github.com/pravbeseda/monitor/internal/sensor/disk"
	"github.com/pravbeseda/monitor/internal/version"
)

// The keys an environment file may supply. tokenVariable is also read from the process
// environment: a secret never lives in a file in the tree.
const (
	hubVariable   = "MONITOR_HUB"
	nodeVariable  = "MONITOR_NODE"
	tokenVariable = "MONITOR_TOKEN"
)

// requestTimeout keeps one unanswered request from swallowing a whole tick.
const requestTimeout = 30 * time.Second

// errVersionRequested is --version answered on stdout: a request, like -h, not a failure.
var errVersionRequested = errors.New("version requested")

func main() {
	if err := start(); err != nil {
		// -h and --version have already printed their answer; both are requests.
		if errors.Is(err, flag.ErrHelp) || errors.Is(err, errVersionRequested) {
			return
		}
		fmt.Fprintf(os.Stderr, "agent: %v\n", err)
		os.Exit(1)
	}
}

// start owns the signal context, so that main holds nothing that os.Exit would skip.
func start() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// options are everything a node holds locally (ADR 0010): nothing else has a default,
// because everything else comes from the hub.
type options struct {
	hub   string
	node  string
	token string
}

func run(ctx context.Context, args []string, out io.Writer) error {
	opts, err := settings(args, out)
	if err != nil {
		return err
	}

	// The sensor reads the allow-list the hub last delivered, so it closes over the agent
	// that is built from it.
	var running *agent.Agent
	volumes := disk.New(disk.System(), func() disk.Settings {
		return disk.Settings{Filesystems: running.Filesystems(), SkipMounts: running.SkipMounts()}
	}, time.Now)
	running = agent.New(agent.Options{
		Node:    opts.node,
		Sensors: []sensor.Sensor{volumes},
		Client:  agent.NewHTTPClient(opts.hub, opts.token, requestTimeout),
		Now:     time.Now,
	})

	if _, err := fmt.Fprintf(out, "monitor-agent %s: node %s reporting to %s\n",
		version.Current, opts.node, opts.hub); err != nil {
		return fmt.Errorf("write to stdout: %w", err)
	}
	return running.Run(ctx)
}

// settings reads the local configuration. Its own errors are printed by the caller, so the
// flag package stays quiet — except for -h, which is answered with the flag list on out.
func settings(args []string, out io.Writer) (options, error) {
	flags := flag.NewFlagSet("agent", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var opts options
	var envFile string
	var showVersion bool
	flags.BoolVar(&showVersion, "version", false, "print the version and exit")
	flags.StringVar(&opts.hub, "hub", "", "base URL of the hub")
	flags.StringVar(&opts.node, "node", "", "this node's name, as the hub knows it")
	flags.StringVar(&envFile, "env-file", "",
		"KEY=VALUE file supplying "+hubVariable+", "+nodeVariable+" and "+tokenVariable)
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
		if _, err := fmt.Fprintf(out, "monitor-agent %s\n", version.Current); err != nil {
			return options{}, fmt.Errorf("write to stdout: %w", err)
		}
		return options{}, errVersionRequested
	}
	opts.token = os.Getenv(tokenVariable)
	if envFile != "" {
		values, err := agent.ReadEnvFile(envFile)
		if err != nil {
			return options{}, err
		}
		// Anything given explicitly wins over the file: cmp.Or takes the first value set.
		opts.hub = cmp.Or(opts.hub, values[hubVariable])
		opts.node = cmp.Or(opts.node, values[nodeVariable])
		opts.token = cmp.Or(opts.token, values[tokenVariable])
	}
	if opts.hub == "" {
		return options{}, errors.New("--hub is required: the hub URL has no default")
	}
	if opts.node == "" {
		return options{}, errors.New("--node is required: it must match the name the hub's token belongs to")
	}
	if opts.token == "" {
		return options{}, fmt.Errorf("%s is unset: the node's token has no default", tokenVariable)
	}
	return opts, nil
}
