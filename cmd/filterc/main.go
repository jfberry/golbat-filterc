// Command filterc compiles filter expressions into Golbat pokemon scan
// requests, and can run them.
//
//	filterc compile [flags] 'expression'   print the v3 request body
//	filterc scan    [flags] 'expression'   compile, call Golbat, print the response
//	filterc serve   [flags]                HTTP server (see serve.go)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jfberry/golbat-filterc/filterc"
	"github.com/jfberry/golbat-filterc/golbat"
)

const usage = `usage:
  filterc compile [flags] 'expression'   print the v3 scan request body
  filterc scan    [flags] 'expression'   compile, run against Golbat, print the response
  filterc serve   [flags]                HTTP server: POST /compile, POST /scan, GET /healthz

flags (all commands; a flag overrides filterc.toml, which overrides the defaults):
  --config path   config file (default ./filterc.toml, then $XDG_CONFIG_HOME/filterc/filterc.toml)
  --min-lat --min-lon --max-lat --max-lon   scan bounds
  --limit n       result limit (0 = Golbat's default)
  --golbat url    Golbat base URL          --secret s   Golbat api_secret
  --listen addr   serve address            --json       compact output
`

// settings is the config with flags applied.
type settings struct {
	Config
	compact bool
}

func parseFlags(args []string, stderr io.Writer) (settings, []string, error) {
	fs := flag.NewFlagSet("filterc", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "")
	minLat, minLon := fs.Float64("min-lat", 0, ""), fs.Float64("min-lon", 0, "")
	maxLat, maxLon := fs.Float64("max-lat", 0, ""), fs.Float64("max-lon", 0, "")
	limit := fs.Int("limit", 0, "")
	golbatURL, secret := fs.String("golbat", "", ""), fs.String("secret", "", "")
	listen := fs.String("listen", "", "")
	compact := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil {
		return settings{}, nil, err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return settings{}, nil, err
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "min-lat":
			cfg.Bounds.Min.Lat = *minLat
		case "min-lon":
			cfg.Bounds.Min.Lon = *minLon
		case "max-lat":
			cfg.Bounds.Max.Lat = *maxLat
		case "max-lon":
			cfg.Bounds.Max.Lon = *maxLon
		case "limit":
			cfg.Bounds.Limit = *limit
		case "golbat":
			cfg.Golbat.URL = *golbatURL
		case "secret":
			cfg.Golbat.Secret = *secret
		case "listen":
			cfg.Listen = *listen
		}
	})
	return settings{Config: cfg, compact: *compact}, fs.Args(), nil
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd := args[0]
	s, rest, err := parseFlags(args[1:], stderr)
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(stderr, "error: %v\n", err)
		}
		return 2
	}
	switch cmd {
	case "compile", "scan":
		if len(rest) != 1 {
			fmt.Fprintf(stderr, "usage: filterc %s [flags] 'expression'\n", cmd)
			return 2
		}
		return runExpression(cmd, s, rest[0], stdout, stderr)
	case "serve":
		return runServe(s, stdout, stderr)
	}
	fmt.Fprint(stderr, usage)
	return 2
}

func runExpression(cmd string, s settings, expression string, stdout, stderr io.Writer) int {
	compiled, err := filterc.Compile(expression)
	if err != nil {
		reportCompileError(stderr, expression, err)
		return 1
	}
	for _, w := range compiled.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	req := compiled.Request(s.bounds(), s.Bounds.Limit)
	if cmd == "compile" {
		return writeJSON(stdout, stderr, req, s.compact)
	}
	if s.Golbat.URL == "" {
		fmt.Fprintln(stderr, "error: scan needs a golbat url (--golbat or [golbat] url in filterc.toml)")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := &golbat.Client{URL: s.Golbat.URL, Secret: s.Golbat.Secret}
	resp, err := client.ScanPokemon(ctx, req)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, resp, s.compact)
}

// reportCompileError prints the error and, for a positioned error, the
// offending line with a caret under the column.
func reportCompileError(stderr io.Writer, expression string, err error) {
	fmt.Fprintf(stderr, "error: %v\n", err)
	var ce *filterc.Error
	if !errors.As(err, &ce) || ce.Pos.Line < 1 {
		return
	}
	lines := strings.Split(expression, "\n")
	if ce.Pos.Line > len(lines) {
		return
	}
	fmt.Fprintf(stderr, "  %s\n  %s^\n", lines[ce.Pos.Line-1], strings.Repeat(" ", max(ce.Pos.Column-1, 0)))
}

func writeJSON(stdout, stderr io.Writer, v any, compact bool) int {
	enc := json.NewEncoder(stdout)
	if !compact {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
