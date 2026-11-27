package library

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"path"
	"strings"
)

const (
	maxNestedZIPBytes   = 8 << 20
	maxNestedStream     = 32 << 20
	maxNestedDictionary = 32 << 20
	maxNestedZIPs       = 16
	maxNestedZIPEntries = 4096
)

func inspectNestedZIPs(reader io.ReaderAt, nextOffset uint64, catalog catalog7z) (romFormats, error) {
	decoder := nestedDecoder{folder: -1, packedLeft: maxNestedStream, decodedLeft: maxNestedStream}
	entriesLeft := maxNestedZIPEntries
	var found romFormats
	var pending []error
	for i, entry := range catalog.zips {
		if i == maxNestedZIPs {
			pending = append(pending, errors.New("limite de ZIPs internos excedido"))
			break
		}
		data, err := decoder.read(reader, nextOffset, catalog.streams, entry)
		if err == nil {
			var formats romFormats
			formats, err = inspectInnerZIP(data, &entriesLeft)
			found = append(found, formats...)
		}
		if err != nil {
			pending = append(pending, fmt.Errorf("ZIP interno %q: %w", entry.name, err))
		}
	}
	return found, errors.Join(pending...)
}

type nestedDecoder struct {
	folder      int
	pos         uint64
	packedLeft  uint64
	decodedLeft uint64
	stream      io.Reader
	err         error
}

func (d *nestedDecoder) read(reader io.ReaderAt, nextOffset uint64, streams streams7z, entry nestedZIP) ([]byte, error) {
	if entry.size == 0 {
		return nil, zip.ErrFormat
	}
	if entry.size > maxNestedZIPBytes {
		return nil, errors.New("limite de tamanho do ZIP interno excedido")
	}
	if d.folder != entry.folder {
		d.folder, d.pos = entry.folder, 0
		d.stream, d.err = d.open(reader, nextOffset, streams)
	}
	if d.err != nil {
		return nil, d.err
	}
	if entry.offset < d.pos {
		return nil, errInvalid7z
	}
	skip := entry.offset - d.pos
	if skip > d.decodedLeft || entry.size > d.decodedLeft-skip {
		return nil, errors.New("limite de leitura descomprimida excedido")
	}
	d.decodedLeft -= skip + entry.size
	if skip != 0 {
		if _, err := io.CopyN(io.Discard, d.stream, int64(skip)); err != nil {
			d.err = err
			return nil, err
		}
	}
	data := make([]byte, int(entry.size))
	if _, err := io.ReadFull(d.stream, data); err != nil {
		d.err = err
		return nil, err
	}
	d.pos = entry.offset + entry.size
	folder := streams.folders[d.folder]
	if folder.streams == 1 && folder.crc != nil && crc32.ChecksumIEEE(data) != *folder.crc {
		return nil, errors.New("CRC do ZIP interno divergente")
	}
	return data, nil
}

func (d *nestedDecoder) open(reader io.ReaderAt, nextOffset uint64, streams streams7z) (io.Reader, error) {
	folder := streams.folders[d.folder]
	for _, codec := range folder.coders {
		if codec.method == "\x06\xf1\x07\x01" {
			return nil, errors.New("fluxo do ZIP interno criptografado")
		}
	}
	if len(folder.coders) != 1 || folder.packed != 1 || folder.outputs != 1 {
		return nil, errors.New("fluxo 7z do ZIP interno sem suporte")
	}
	offset := streams.packOffset
	for _, length := range streams.packSizes[:folder.firstPack] {
		if offset > nextOffset || length > nextOffset-offset {
			return nil, errInvalid7z
		}
		offset += length
	}
	length := streams.packSizes[folder.firstPack]
	if offset > nextOffset || length > nextOffset-offset {
		return nil, errInvalid7z
	}
	if length > d.packedLeft {
		return nil, errors.New("limite de leitura comprimida excedido")
	}
	d.packedLeft -= length
	return open7zStream(folder.coders[0],
		io.NewSectionReader(reader, sevenZipHeaderSize+int64(offset), int64(length)),
		folder.outputSize, maxNestedDictionary)
}

// A ZIP directory is the last level we inspect. No ZIP entry is opened.
func inspectInnerZIP(data []byte, entriesLeft *int) (romFormats, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if len(archive.File) > *entriesLeft {
		return nil, errors.New("limite de entradas de ZIP interno excedido")
	}
	*entriesLeft -= len(archive.File)
	var found romFormats
	var encrypted, deeper bool
	for _, file := range archive.File {
		if file.Flags&1 != 0 {
			encrypted = true
			continue
		}
		if !file.Mode().IsRegular() {
			continue
		}
		extension := strings.ToLower(path.Ext(file.Name))
		if knownROMExtension(extension) {
			found.add(extension)
		}
		deeper = deeper || extension == ".zip" || extension == ".7z"
	}
	var pending []error
	if encrypted {
		pending = append(pending, errors.New("entrada ZIP criptografada"))
	}
	if deeper {
		pending = append(pending, errors.New("limite de profundidade excedido"))
	}
	return found, errors.Join(pending...)
}
