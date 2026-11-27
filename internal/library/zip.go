package library

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
)

// zipROMFormats inspects only the ZIP directory. It never opens an entry.
func zipROMFormats(root fs.FS, name string) (romFormats, error) {
	file, err := root.Open(strings.TrimPrefix(name, "/"))
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	reader, ok := file.(io.ReaderAt)
	if !ok || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("leitura aleatória indisponível para este arquivo")
	}
	archive, err := zip.NewReader(reader, info.Size())
	if err != nil {
		return nil, err
	}
	var formats romFormats
	var encrypted, nested bool
	for _, entry := range archive.File {
		if entry.Flags&1 != 0 {
			encrypted = true
			continue
		}
		extension := strings.ToLower(path.Ext(entry.Name))
		if !entry.Mode().IsRegular() {
			continue
		}
		if knownROMExtension(extension) {
			formats.add(extension)
		}
		nested = nested || extension == ".zip" || extension == ".7z" || extension == ".rar"
	}
	var limitations []error
	if encrypted {
		limitations = append(limitations, errors.New("entrada ZIP criptografada"))
	}
	if nested {
		limitations = append(limitations, errors.New("pacote dentro de ZIP não inspecionado"))
	}
	return formats, errors.Join(limitations...)
}
