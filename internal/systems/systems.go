package systems

import (
	"errors"
	"io/fs"
	"path"
	"strings"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
	"github.com/WilliamO0o/Stark-Manager/internal/library"
)

type Report struct {
	SchemaVersion int                   `json:"schema_version"`
	CatalogStatus string                `json:"catalog_status"`
	Catalogs      []Catalog             `json:"catalogs"`
	Folders       []Folder              `json:"folders"`
	NotExamined   []library.NotExamined `json:"not_examined"`
	Pending       []library.Pending     `json:"pending"`
	Limitations   []string              `json:"limitations"`
}

type Folder struct {
	Path       string     `json:"path"`
	Assessment Assessment `json:"assessment"`
}

type Assessment struct {
	Status    string                      `json:"status"`
	Path      string                      `json:"resolved_path,omitempty"`
	Reason    string                      `json:"reason,omitempty"`
	Partition *string                     `json:"partition"`
	Device    *string                     `json:"device"`
	Binds     []diagnostic.ConfiguredBind `json:"binds"`
}

type probe struct {
	mountAt func(string) *diagnostic.Mount
	lstat   func(string) (fs.FileInfo, error)
	binds   []diagnostic.ConfiguredBind
}

// Collect compares each candidate catalog with visible libraries, using the
// supplied mount namespace and a metadata reader that does not follow symlinks.
func Collect(root fs.FS, storage diagnostic.StorageReport, lstat func(string) (fs.FileInfo, error), home, configHome string) Report {
	report := Report{
		SchemaVersion: 1, Catalogs: readCatalogs(root, home, configHome),
		Folders: []Folder{}, Pending: []library.Pending{}, Limitations: []string{},
		NotExamined: library.UnexaminedCards(root, storage),
	}
	report.Limitations = append(report.Limitations, storage.Mounts.Issues...)
	report.Limitations = append(report.Limitations, storage.Devices.Issues...)
	report.Limitations = append(report.Limitations, storage.Configured.Issues...)
	switch {
	case len(report.Catalogs) == 0:
		report.CatalogStatus = "missing"
		report.Limitations = append(report.Limitations, "es_systems.cfg não encontrado nos locais consultados")
	case len(report.Catalogs) > 1:
		report.CatalogStatus = "ambiguous"
		report.Limitations = append(report.Limitations, "há mais de um candidato; configuração ativa indeterminada")
	default:
		report.CatalogStatus = report.Catalogs[0].Status
	}
	p := probe{mountAt: storage.MountLookup(), lstat: lstat}
	if storage.Configured.Data != nil {
		p.binds = *storage.Configured.Data
	}
	var roots []string
	for _, candidate := range library.DiscoverRoots(storage, p.mountAt) {
		if p.unmountedBindAt(candidate.Path) {
			report.Pending = append(report.Pending, library.Pending{
				Path: candidate.Path, Reason: "destino de bind sem montagem observada",
			})
			continue
		}
		info, err := diagnostic.CheckMountedPath(p.mountAt, candidate.Path, lstat)
		if err != nil || !info.IsDir() {
			if candidate.Observed || (err != nil && !errors.Is(err, fs.ErrNotExist)) {
				reason := "o caminho não é um diretório"
				if err != nil {
					reason = err.Error()
				}
				report.Pending = append(report.Pending, library.Pending{Path: candidate.Path, Reason: reason})
			}
			continue
		}
		roots = append(roots, candidate.Path)
		entries, readErr := fs.ReadDir(root, strings.TrimPrefix(candidate.Path, "/"))
		if readErr != nil {
			report.Pending = append(report.Pending, library.Pending{Path: candidate.Path, Reason: readErr.Error()})
		}
		for _, entry := range entries {
			if entry.IsDir() {
				report.Folders = append(report.Folders, Folder{Path: path.Join(candidate.Path, entry.Name())})
			}
		}
	}

	var declared []string
	for i := range report.Catalogs {
		catalog := &report.Catalogs[i]
		if catalog.Issue != "" {
			report.Limitations = append(report.Limitations, catalog.Path+": "+catalog.Issue)
		}
		for j := range catalog.Systems {
			system := &catalog.Systems[j]
			name := resolve(system.Path, home)
			if name != "" && report.CatalogStatus == "ok" {
				declared = append(declared, name)
			}
			system.Location = p.configuredPath(name, roots)
		}
	}
	for i := range report.Folders {
		folder := &report.Folders[i]
		folder.Assessment = p.assessment(folder.Path)
		if p.unmountedBindAt(folder.Path) {
			folder.Assessment.Status = "not_examined"
			folder.Assessment.Reason = "destino de bind sem montagem observada"
			folder.Assessment.Device, folder.Assessment.Partition = nil, nil
			continue
		}
		folder.Assessment.Status = "catalog_indeterminate"
		if report.CatalogStatus != "ok" {
			continue
		}
		folder.Assessment.Status = "undeclared"
		for _, name := range declared {
			if within(name, folder.Path) || within(folder.Path, name) {
				folder.Assessment.Status = "declared"
				break
			}
		}
	}
	return report
}

func (p probe) assessment(name string) Assessment {
	result := Assessment{Path: name, Binds: []diagnostic.ConfiguredBind{}}
	if mount := p.mountAt(name); name != "" && mount != nil {
		result.Partition, result.Device = mount.Partition, mount.Device
	}
	for _, bind := range p.binds {
		if bind.Status == diagnostic.BindCompatible &&
			(within(name, resolve(bind.Target, "")) || within(name, resolve(bind.Source, ""))) {
			result.Binds = append(result.Binds, bind)
		}
	}
	return result
}

func (p probe) configuredPath(name string, roots []string) Assessment {
	result := p.assessment(name)
	result.Status = "not_examined"
	if name == "" {
		result.Reason = "caminho vazio ou sem uma base absoluta verificável"
		return result
	}
	if p.unmountedBindAt(name) {
		result.Device, result.Partition = nil, nil
		result.Reason = "o destino do bind configurado está sem montagem observada"
		return result
	}
	info, err := diagnostic.CheckMountedPath(p.mountAt, name, p.lstat)
	switch {
	case err == nil && info.IsDir():
		result.Status = "present"
	case err == nil:
		result.Status, result.Reason = "missing", "o caminho existe, mas não é um diretório"
	case errors.Is(err, fs.ErrNotExist):
		result.Reason = "sem uma biblioteca montada confirmada nesse caminho"
		for _, root := range roots {
			if within(name, root) {
				result.Status, result.Reason = "missing", "diretório ausente na biblioteca examinada"
				break
			}
		}
	default:
		result.Reason = err.Error()
	}
	return result
}

func (p probe) unmountedBindAt(name string) bool {
	for _, bind := range p.binds {
		if bind.Status == diagnostic.BindNotObserved && within(name, resolve(bind.Target, "")) {
			return true
		}
	}
	return false
}

func resolve(name, home string) string {
	name = strings.TrimSpace(name)
	if suffix, ok := strings.CutPrefix(name, "~/"); ok && path.IsAbs(home) {
		name = strings.TrimSuffix(home, "/") + "/" + suffix
	}
	if !path.IsAbs(name) || strings.ContainsRune(name, 0) {
		return ""
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return ""
		}
	}
	return path.Clean(name)
}

func within(name, base string) bool {
	return name != "" && base != "" && (name == base || strings.HasPrefix(name, strings.TrimSuffix(base, "/")+"/"))
}
