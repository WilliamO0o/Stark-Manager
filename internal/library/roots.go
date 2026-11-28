package library

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
)

// LocalSource identifies a backup selected by path, independently of its backing disk.
type LocalSource struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	OriginalPath string `json:"original_path"`
}

func (s *scanner) discoverLocalSources() {
	seen := make(map[string]bool)
	for _, original := range s.options.Roots {
		if original == "" {
			s.pending(original, "caminho de backup vazio")
			continue
		}
		name, err := filepath.Abs(original)
		if err != nil {
			s.pending(original, err.Error())
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		s.report.LocalSources = append(s.report.LocalSources, &LocalSource{
			Name: filepath.Base(name), Path: name, OriginalPath: original,
		})
	}
	if s.options.Cache != nil {
		if err := s.checkLocalCache(); err != nil {
			s.report.Limitations = append(s.report.Limitations, "cache: "+err.Error())
			s.options.Cache = nil
		}
	}
	for _, source := range s.report.LocalSources {
		s.scanRoot(RootCandidate{Path: source.Path, Observed: true}, source)
	}
}

// The inventory backend also enforces this boundary for callers outside the CLI.
func (s *scanner) checkLocalCache() error {
	cache, err := filepath.Abs(s.options.Cache.Dir)
	if err != nil {
		return err
	}
	cache = resolvedLocation(cache)
	cacheMount := s.mountAt(cache)
	for _, source := range s.report.LocalSources {
		root := resolvedLocation(source.Path)
		if withinPath(cache, root) {
			return errors.New("diretório de cache dentro da biblioteca local")
		}
		if mount := s.mountAt(root); mount != nil && cacheMount != nil && mount.MajorMinor == cacheMount.MajorMinor &&
			withinPath(fileLocation(cacheMount, cache).root, fileLocation(mount, root).root) {
			return errors.New("diretório de cache aponta para a biblioteca local")
		}
	}
	return nil
}

func withinPath(name, folder string) bool {
	return name == folder || strings.HasPrefix(name, strings.TrimSuffix(folder, "/")+"/")
}

func resolvedLocation(name string) string {
	if resolved, err := filepath.EvalSymlinks(name); err == nil {
		return resolved
	}
	parent := filepath.Dir(name)
	if parent == name {
		return name
	}
	return filepath.Join(resolvedLocation(parent), filepath.Base(name))
}

var unlinkedLocalMount = diagnostic.Mount{Target: "/", Root: "/", Filesystem: "local"}

func (s *scanner) scanMount(name string, source *LocalSource) *diagnostic.Mount {
	mount := s.mountAt(name)
	if mount == nil && source != nil {
		return &unlinkedLocalMount
	}
	return mount
}

func (s *scanner) sourceLocation(mount *diagnostic.Mount, name string) location {
	loc := fileLocation(mount, name)
	if s.active.source != nil {
		loc.source = s.active.source.Path
	}
	return loc
}
