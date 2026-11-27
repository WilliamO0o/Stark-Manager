package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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
		fmt.Fprintln(w, "     stark library [sistema] [--json] [--refresh]\n     stark library {cache|warnings} [sistema] [--json]")
	}
	// Preserve the JSON output of the original, argument-free invocation.
	asJSON := len(args) == 0
	var filter, libraryMode string
	var force bool
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help", "help":
			usage(stdout)
			return 0
		case "library":
			libraryArgs := args[1:]
			if len(libraryArgs) > 0 && (libraryArgs[0] == "cache" || libraryArgs[0] == "warnings") {
				libraryMode, libraryArgs = libraryArgs[0], libraryArgs[1:]
			}
			for _, arg := range libraryArgs {
				switch {
				case arg == "--help" || arg == "-h":
					fmt.Fprintln(stdout, "Uso: stark library [sistema] [--json] [--refresh]\n     stark library {cache|warnings} [sistema] [--json]")
					return 0
				case arg == "--json":
					asJSON = true
				case arg == "--refresh" && libraryMode == "":
					force = true
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
		err = runLibrary(root, stdout, stderr, encoder, filter, libraryMode, asJSON, force)
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

func runLibrary(root fs.FS, stdout, stderr io.Writer, encoder *json.Encoder, filter, mode string, asJSON, force bool) error {
	base, cacheErr := os.UserCacheDir()
	cache := library.Cache{Dir: filepath.Join(base, "stark-manager")}
	if mode != "" {
		if cacheErr != nil {
			return cacheErr
		}
		report := presentation.ReadCacheReport(cache, filter, mode == "warnings")
		if asJSON {
			return encoder.Encode(report)
		}
		return presentation.WriteCacheReport(stdout, report, mode == "warnings")
	}
	var progress *presentation.LibraryProgress
	options := library.UpdateOptions{Force: force, VolumeID: library.LocalVolumeID}
	if !asJSON && presentation.IsTerminal(stdout) && presentation.IsTerminal(stderr) {
		progress = presentation.NewLibraryProgress(stderr, filter)
		options.Progress = progress.Event
	} else if !asJSON {
		options.Progress = func(event library.Event) {
			if event.Phase == "cached" {
				presentation.WriteCachedFolder(stdout, *event.Result)
			}
		}
	}
	storage := diagnostic.CollectStorage(root, nil)
	if cacheErr == nil {
		cacheErr = validateCachePath(cache.Dir, storage)
		if cacheErr == nil {
			options.Cache = &cache
		}
	}
	home, _ := os.UserHomeDir()
	catalog := systems.LibraryCatalog(root, home, os.Getenv("XDG_CONFIG_HOME"))
	report := library.Update(root, storage, os.Lstat, filter, options, catalog...)
	if cacheErr != nil {
		report.Limitations = append(report.Limitations, "cache: "+cacheErr.Error())
	}
	if asJSON {
		return encoder.Encode(report)
	}
	if progress != nil {
		progress.Finish()
		return presentation.WriteLibraryNotes(stdout, report)
	}
	return presentation.WriteLibrary(stdout, report)
}

func validateCachePath(dir string, storage diagnostic.StorageReport) error {
	resolved := resolveCachePath(dir)
	mountAt := storage.MountLookup()
	cacheMount := mountAt(resolved)
	if cacheMount != nil && cacheMount.Device != nil && strings.HasPrefix(*cacheMount.Device, "mmcblk") {
		return fmt.Errorf("diretório de cache em dispositivo MMC")
	}
	for _, root := range library.DiscoverRoots(storage, mountAt) {
		if cacheMount != nil {
			if libraryMount := mountAt(root.Path); libraryMount != nil && cacheMount.MajorMinor == libraryMount.MajorMinor {
				volumeCache := filepath.Join(cacheMount.Root, strings.TrimPrefix(resolved, cacheMount.Target))
				volumeLibrary := filepath.Join(libraryMount.Root, strings.TrimPrefix(root.Path, libraryMount.Target))
				if volumeCache == volumeLibrary || strings.HasPrefix(volumeCache, strings.TrimSuffix(volumeLibrary, "/")+"/") {
					return fmt.Errorf("diretório de cache aponta para uma biblioteca de ROMs")
				}
			}
		}
		for _, folder := range []string{root.Path, resolveCachePath(root.Path)} {
			if resolved == folder || strings.HasPrefix(resolved, folder+"/") ||
				dir == folder || strings.HasPrefix(dir, folder+"/") {
				return fmt.Errorf("diretório de cache dentro de uma biblioteca de ROMs")
			}
		}
	}
	return nil
}

func resolveCachePath(name string) string {
	if resolved, err := filepath.EvalSymlinks(name); err == nil {
		return resolved
	}
	parent := filepath.Dir(name)
	if parent == name {
		return name
	}
	return filepath.Join(resolveCachePath(parent), filepath.Base(name))
}
