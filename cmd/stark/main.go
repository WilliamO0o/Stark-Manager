package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
	"github.com/WilliamO0o/Stark-Manager/internal/library"
	"github.com/WilliamO0o/Stark-Manager/internal/presentation"
	"github.com/WilliamO0o/Stark-Manager/internal/systems"
)

const version = "0.2.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	usage := func(w io.Writer) {
		fmt.Fprintln(w, "Uso: stark {status|storage|systems} [--json]")
		fmt.Fprintln(w, "     stark library [sistema] [--json]")
	}
	// Preserve the JSON output of the original, argument-free invocation.
	asJSON := len(args) == 0
	var filter string
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help", "help":
			usage(stdout)
			return 0
		case "library":
			for _, arg := range args[1:] {
				switch {
				case arg == "--help" || arg == "-h":
					fmt.Fprintln(stdout, "Uso: stark library [sistema] [--json]")
					return 0
				case arg == "--json":
					asJSON = true
				case filter == "" && arg != "" && arg[0] != '-':
					filter = arg
				default:
					fmt.Fprintf(stderr, "stark: argumento inesperado: %s\n", arg)
					return 2
				}
			}
		case "status", "storage", "systems":
			flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
			flags.SetOutput(stderr)
			flags.Usage = func() { fmt.Fprintf(stderr, "Uso: stark %s [--json]\n", args[0]) }
			flags.BoolVar(&asJSON, "json", false, "exibe o relatório JSON")
			if err := flags.Parse(args[1:]); err != nil {
				if err == flag.ErrHelp {
					return 0
				}
				return 2
			}
			if flags.NArg() != 0 {
				fmt.Fprintf(stderr, "stark: argumento inesperado: %s\n", flags.Arg(0))
				flags.Usage()
				return 2
			}
		default:
			fmt.Fprintf(stderr, "stark: comando desconhecido: %s\n", args[0])
			usage(stderr)
			return 2
		}
	}

	if runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "stark: o diagnóstico local requer Linux")
		return 1
	}
	root := os.DirFS("/")
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	var err error
	if len(args) > 0 && args[0] == "systems" {
		home, _ := os.UserHomeDir()
		report := systems.Collect(root, diagnostic.CollectStorage(root, os.Lstat), os.Lstat, home, os.Getenv("XDG_CONFIG_HOME"))
		if asJSON {
			err = encoder.Encode(report)
		} else {
			err = presentation.WriteSystems(stdout, report)
		}
	} else if len(args) > 0 && args[0] == "library" {
		storage := diagnostic.CollectStorage(root, nil)
		report := library.Collect(root, storage, os.Lstat, filter)
		if asJSON {
			err = encoder.Encode(report)
		} else {
			err = presentation.WriteLibrary(stdout, report)
		}
	} else if len(args) > 0 && args[0] == "storage" {
		report := diagnostic.CollectStorage(root, os.Lstat)
		if asJSON {
			err = encoder.Encode(report)
		} else {
			err = presentation.WriteStorage(stdout, report)
		}
	} else {
		report := diagnostic.Collect(root, runtime.GOARCH)
		if asJSON {
			err = encoder.Encode(report)
		} else {
			err = presentation.WriteStatus(stdout, version, report)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "stark:", err)
		return 1
	}
	return 0
}
