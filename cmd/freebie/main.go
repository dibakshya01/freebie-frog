// Command freebie is the freebie-frog CLI: an Anthropic-compatible,
// multi-provider proxy for coding-agent CLIs.
//
// M0 ships the operational skeleton (serve/config/doctor/version). The
// translating proxy itself is added in later milestones (see PLAN.md).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/dibakshya01/freebie-frog/internal/config"
	"github.com/dibakshya01/freebie-frog/internal/obs"
	"github.com/dibakshya01/freebie-frog/internal/server"
	"github.com/dibakshya01/freebie-frog/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stderr)
		return nil
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "serve":
		return cmdServe(rest)
	case "config":
		return cmdConfig(rest)
	case "doctor":
		return cmdDoctor(rest)
	case "version", "-v", "--version":
		fmt.Println("freebie-frog", version.Version)
		return nil
	case "help", "-h", "--help":
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "path to JSON config file (default: ./config.json if present)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(resolveConfigPath(*cfgPath))
	if err != nil {
		return err
	}
	logger := obs.NewLogger(cfg.Log.Level, cfg.Log.Format)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return server.New(cfg.Server, logger).Run(ctx)
}

func cmdConfig(args []string) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "path to JSON config file (default: ./config.json if present)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(resolveConfigPath(*cfgPath))
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(cfg)
}

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "path to JSON config file (default: ./config.json if present)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Println("freebie-frog doctor")
	fmt.Printf("  version:    %s\n", version.Version)
	fmt.Printf("  go runtime: %s (%s/%s)\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)

	cfg, err := config.Load(resolveConfigPath(*cfgPath))
	if err != nil {
		fmt.Printf("  [FAIL] config: %v\n", err)
		return err
	}
	fmt.Println("  [ok]   config loaded and valid")

	ln, err := net.Listen("tcp", cfg.Server.Addr)
	if err != nil {
		fmt.Printf("  [FAIL] cannot bind %s: %v\n", cfg.Server.Addr, err)
		return err
	}
	_ = ln.Close()
	fmt.Printf("  [ok]   address %s is bindable\n", cfg.Server.Addr)

	fmt.Println("all checks passed")
	return nil
}

// resolveConfigPath returns the explicit flag value if set; otherwise it uses
// ./config.json when that file exists, or "" to run on defaults + env only.
func resolveConfigPath(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	const auto = "config.json"
	if _, err := os.Stat(auto); err == nil {
		return auto
	}
	return ""
}

func usage(w *os.File) {
	fmt.Fprint(w, `freebie-frog — Anthropic-compatible multi-provider proxy

Usage:
  freebie <command> [flags]

Commands:
  serve     Start the proxy server
  config    Print the resolved configuration and exit
  doctor    Run environment/config diagnostics
  version   Print the version
  help      Show this help

Flags (serve/config/doctor):
  --config <path>   Path to a JSON config file (defaults to ./config.json if present)

Environment overrides (take precedence over the file):
  FREEBIE_SERVER_ADDR
  FREEBIE_LOG_LEVEL                (debug|info|warn|error)
  FREEBIE_LOG_FORMAT               (text|json)
  FREEBIE_SERVER_READ_TIMEOUT      (Go duration, e.g. 30s)
  FREEBIE_SERVER_WRITE_TIMEOUT     (Go duration; 0 = none, recommended for streaming)
  FREEBIE_SERVER_SHUTDOWN_TIMEOUT  (Go duration, e.g. 15s)
`)
}
