// Package systems diagnoses EmulationStation catalogs without executing commands.
package systems

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
)

type Catalog struct {
	Path    string   `json:"path"`
	Read    bool     `json:"read"`
	Status  string   `json:"status"`
	Issue   string   `json:"issue,omitempty"`
	Systems []System `json:"systems"`
}

type System struct {
	Name      string     `xml:"name" json:"name"`
	FullName  string     `xml:"fullname" json:"fullname"`
	Path      string     `xml:"path" json:"path"`
	Extension string     `xml:"extension" json:"extension"`
	Location  Assessment `xml:"-" json:"location"`
}

func readCatalogs(root fs.FS, home, configHome string) []Catalog {
	candidates := map[string]bool{
		"/etc/emulationstation/es_systems.cfg":                      true,
		"/usr/share/emulationstation/es_systems.cfg":                true,
		"/opt/retropie/configs/all/emulationstation/es_systems.cfg": true,
	}
	if path.IsAbs(home) {
		candidates[path.Join(home, ".emulationstation/es_systems.cfg")] = true
		candidates[path.Join(home, ".config/emulationstation/es_systems.cfg")] = true
	}
	if path.IsAbs(configHome) {
		candidates[path.Join(configHome, "emulationstation/es_systems.cfg")] = true
	}
	names := make([]string, 0, len(candidates))
	for name := range candidates {
		names = append(names, name)
	}
	sort.Strings(names)

	catalogs := []Catalog{}
	for _, name := range names {
		data, err := fs.ReadFile(root, strings.TrimPrefix(name, "/"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		catalog := Catalog{Path: name, Systems: []System{}}
		if err != nil {
			catalog.Status, catalog.Issue = "unreadable", err.Error()
		} else {
			catalog.Read = true
			records, err := decodeCatalog(string(data))
			if err != nil {
				catalog.Status, catalog.Issue = "invalid", err.Error()
			} else {
				catalog.Status, catalog.Systems = "ok", records
			}
		}
		catalogs = append(catalogs, catalog)
	}
	return catalogs
}

func decodeCatalog(data string) ([]System, error) {
	document := struct {
		XMLName xml.Name `xml:"systemList"`
		Systems []System `xml:"system"`
	}{Systems: []System{}}
	decoder := xml.NewDecoder(strings.NewReader(data))
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if start, ok := token.(xml.StartElement); ok {
			if err := decoder.DecodeElement(&document, &start); err != nil {
				return nil, err
			}
			break
		}
		switch value := token.(type) {
		case xml.Comment, xml.ProcInst, xml.Directive:
			continue
		case xml.CharData:
			if strings.TrimSpace(string(value)) == "" {
				continue
			}
		}
		return nil, fmt.Errorf("conteúdo inesperado antes de systemList")
	}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return document.Systems, nil
		}
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.Comment, xml.ProcInst:
			continue
		case xml.CharData:
			if strings.TrimSpace(string(value)) == "" {
				continue
			}
		}
		return nil, fmt.Errorf("conteúdo inesperado após systemList")
	}
}
