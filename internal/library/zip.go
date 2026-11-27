package library

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
)

// zipHasSNESROM inspects only the ZIP directory. It never opens an entry.
func zipHasSNESROM(root fs.FS, name string) (bool, error) {
	file, err := root.Open(strings.TrimPrefix(name, "/"))
	if err != nil {
		return false, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	reader, ok := file.(io.ReaderAt)
	if !ok || !info.Mode().IsRegular() {
		return false, fmt.Errorf("leitura aleatória indisponível para este arquivo")
	}
	archive, err := zip.NewReader(reader, info.Size())
	if err != nil {
		return false, err
	}
	for _, entry := range archive.File {
		if entry.Mode().IsRegular() && snesROMExtension(strings.ToLower(path.Ext(entry.Name))) {
			return true, nil
		}
	}
	return false, nil
}
