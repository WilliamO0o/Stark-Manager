package library

import (
	_ "embed"
	"path"
	"strings"
)

// families.tsv is maintained by Stark. Columns are family, type, path relative
// to a ROM root, catalog ID, and catalog display name.
//
//go:embed families.tsv
var familyTable string

type CatalogEntry struct {
	Name         string `json:"name"`
	FullName     string `json:"fullname"`
	Path         string `json:"path"`
	Extension    string `json:"extension"`
	ResolvedPath string `json:"-"`
}

type familyKind struct{ family, kind string }

type familyIdentity struct {
	familyKind
	entries []CatalogEntry
}

type familyKey struct{ relative, name, fullName string }

var exactFamilies, standardFamilies = loadFamilies()

func loadFamilies() (map[familyKey]familyKind, map[string]familyKind) {
	exact := make(map[familyKey]familyKind)
	standard := make(map[string]familyKind)
	for _, row := range strings.Split(strings.TrimSpace(familyTable), "\n") {
		fields := strings.Split(row, "\t")
		if len(fields) != 5 {
			panic("invalid library family mapping")
		}
		mapping := familyKind{fields[0], fields[1]}
		key := familyKey{fields[2], normalized(fields[3]), normalized(fields[4])}
		if previous, exists := exact[key]; exists && previous != mapping {
			panic("conflicting library family mapping")
		}
		exact[key] = mapping
		head, _, _ := strings.Cut(fields[2], "/")
		if previous, exists := standard[head]; exists && previous != mapping {
			panic("conflicting standard library folder")
		}
		standard[head] = mapping
	}
	return exact, standard
}

func normalized(name string) string { return strings.ToLower(strings.Join(strings.Fields(name), " ")) }

// Identify each physical folder using all catalog records declared beneath it.
// An unrecognized or conflicting record prevents a standard-folder fallback.
func identifyFolder(root, folder string, entries []CatalogEntry) familyIdentity {
	var identity familyIdentity
	prefix := root + "/" + folder
	for _, entry := range entries {
		if entry.ResolvedPath != prefix && !strings.HasPrefix(entry.ResolvedPath, prefix+"/") {
			continue
		}
		relative := strings.TrimPrefix(entry.ResolvedPath, root+"/")
		found := exactFamilies[familyKey{relative, normalized(entry.Name), normalized(entry.FullName)}]
		if len(identity.entries) == 0 {
			identity.familyKind = found
		} else if identity.familyKind != found {
			identity.familyKind = familyKind{}
		}
		identity.entries = append(identity.entries, entry)
	}
	if len(identity.entries) == 0 {
		identity.familyKind = standardFamilies[folder]
	}
	return identity
}

func (s *scanner) identifyGroup(system *System) {
	var mapping familyKind
	seenFolders := make(map[string]bool)
	for i, name := range system.Paths {
		for source := name; source != "/"; source = path.Dir(source) {
			identity, ok := s.identities[source]
			if !ok {
				continue
			}
			if i == 0 {
				mapping = identity.familyKind
			} else if identity.familyKind != mapping {
				mapping = familyKind{}
			}
			if !seenFolders[source] {
				seenFolders[source] = true
				system.CatalogEntries = append(system.CatalogEntries, identity.entries...)
			}
			break
		}
	}
	system.Family, system.Type = mapping.family, mapping.kind
	if system.Family == "" {
		system.Family, system.Type = "unidentified", "unidentified"
	}
	if len(system.CatalogEntries) == 1 && system.Family != "unidentified" {
		system.DisplayName = strings.TrimSpace(system.CatalogEntries[0].FullName)
	}
}
