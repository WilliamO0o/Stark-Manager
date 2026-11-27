package library

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/ulikunitz/xz/lzma"
)

// 7z indexes may themselves be compressed. Bound their input, output and
// decoder dictionary independently from the limits for embedded ZIPs.
const (
	sevenZipHeaderSize = 32
	maxIndexBytes      = 4 << 20
	maxDictionary      = 8 << 20
	maxIndexEntries    = 16_384

	idEnd        = 0x00
	idHeader     = 0x01
	idProperties = 0x02
	idAdditional = 0x03
	idMain       = 0x04
	idFiles      = 0x05
	idPack       = 0x06
	idUnpack     = 0x07
	idSubstreams = 0x08
	idSize       = 0x09
	idCRC        = 0x0a
	idFolder     = 0x0b
	idUnpackSize = 0x0c
	idNumStreams = 0x0d
	idEmpty      = 0x0e
	idEmptyFile  = 0x0f
	idAnti       = 0x10
	idName       = 0x11
	idAttributes = 0x15
	idEncoded    = 0x17
)

var (
	errInvalid7z     = errors.New("índice 7z inválido")
	errLimited7z     = errors.New("índice 7z excede o limite de leitura")
	errEncrypted7z   = errors.New("índice 7z criptografado")
	errUnsupported7z = errors.New("estrutura ou compressão de índice 7z sem suporte")
)

func sevenZipROMFormats(root fs.FS, name string) (romFormats, error) {
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
		return nil, errUnsupported7z
	}
	return inspect7z(reader, info.Size())
}

func inspect7z(reader io.ReaderAt, size int64) (romFormats, error) {
	var signature [sevenZipHeaderSize]byte
	if _, err := reader.ReadAt(signature[:], 0); err != nil {
		return nil, err
	}
	if string(signature[:6]) != "7z\xbc\xaf\x27\x1c" || signature[6] != 0 ||
		crc32.ChecksumIEEE(signature[12:]) != binary.LittleEndian.Uint32(signature[8:12]) {
		return nil, errInvalid7z
	}
	offset := binary.LittleEndian.Uint64(signature[12:20])
	length := binary.LittleEndian.Uint64(signature[20:28])
	if length > maxIndexBytes {
		return nil, errLimited7z
	}
	if size < sevenZipHeaderSize || offset > uint64(size-sevenZipHeaderSize) ||
		length > uint64(size-sevenZipHeaderSize)-offset {
		return nil, errInvalid7z
	}
	if length == 0 && binary.LittleEndian.Uint32(signature[28:]) == 0 {
		return nil, nil // A valid empty archive has no next header.
	}
	index := make([]byte, int(length))
	if _, err := reader.ReadAt(index, sevenZipHeaderSize+int64(offset)); err != nil {
		return nil, err
	}
	if crc32.ChecksumIEEE(index) != binary.LittleEndian.Uint32(signature[28:]) {
		return nil, errInvalid7z
	}
	if len(index) == 0 {
		return nil, errInvalid7z
	}
	if index[0] == idEncoded {
		var err error
		index, err = decode7zIndex(reader, offset, index[1:])
		if err != nil {
			return nil, err
		}
	}
	catalog, err := inspect7zNames(index)
	if err != nil {
		return nil, err
	}
	nested, pending := inspectNestedZIPs(reader, offset, catalog)
	return append(catalog.formats, nested...), pending
}

func decode7zIndex(reader io.ReaderAt, nextOffset uint64, index []byte) ([]byte, error) {
	c := cursor7z{data: index}
	streams := read7zStreams(&c)
	if c.err != nil {
		return nil, c.err
	}
	for _, folder := range streams.folders {
		for _, codec := range folder.coders {
			if codec.method == "\x06\xf1\x07\x01" {
				return nil, errEncrypted7z
			}
		}
	}
	if len(c.data) != 0 || len(streams.folders) != 1 || len(streams.folders[0].coders) != 1 || len(streams.packSizes) != 1 {
		return nil, errUnsupported7z
	}
	folder := streams.folders[0]
	codec := folder.coders[0]
	length := streams.packSizes[0]
	if length > maxIndexBytes || folder.outputSize > maxIndexBytes {
		return nil, errLimited7z
	}
	if streams.packOffset > nextOffset || length > nextOffset-streams.packOffset || folder.outputSize == 0 {
		return nil, errInvalid7z
	}
	packed := make([]byte, int(length))
	if _, err := reader.ReadAt(packed, sevenZipHeaderSize+int64(streams.packOffset)); err != nil {
		return nil, err
	}
	if streams.packCRC != nil && crc32.ChecksumIEEE(packed) != *streams.packCRC {
		return nil, errInvalid7z
	}
	decoded, err := unpack7zIndex(codec, packed, folder.outputSize)
	if err != nil {
		return nil, err
	}
	if folder.crc != nil && crc32.ChecksumIEEE(decoded) != *folder.crc {
		return nil, errInvalid7z
	}
	return decoded, nil
}

func unpack7zIndex(codec coder7z, packed []byte, size uint64) ([]byte, error) {
	stream, err := open7zStream(codec, bytes.NewReader(packed), size, maxDictionary)
	if err != nil {
		return nil, err
	}
	decoded, err := io.ReadAll(io.LimitReader(stream, int64(size)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(decoded)) != size {
		return nil, errInvalid7z
	}
	return decoded, nil
}

func open7zStream(codec coder7z, packed io.Reader, size, dictionaryLimit uint64) (io.Reader, error) {
	switch codec.method {
	case "\x00": // Copy
		return packed, nil
	case "\x03\x01\x01": // LZMA
		if len(codec.properties) != 5 {
			return nil, errInvalid7z
		}
		if uint64(binary.LittleEndian.Uint32(codec.properties[1:])) > dictionaryLimit {
			return nil, errLimited7z
		}
		var header [13]byte
		copy(header[:], codec.properties)
		binary.LittleEndian.PutUint64(header[5:], size)
		return lzma.ReaderConfig{DictCap: int(dictionaryLimit)}.NewReader(io.MultiReader(bytes.NewReader(header[:]), packed))
	case "\x21": // LZMA2
		if len(codec.properties) != 1 || codec.properties[0] > 40 {
			return nil, errInvalid7z
		}
		prop := codec.properties[0]
		dictionary := uint64(2|prop&1) << (prop/2 + 11)
		if dictionary > dictionaryLimit {
			return nil, errLimited7z
		}
		return lzma.Reader2Config{DictCap: int(dictionary)}.NewReader2(packed)
	default:
		return nil, errUnsupported7z
	}
}

type catalog7z struct {
	streams streams7z
	formats romFormats
	zips    []nestedZIP
}

type nestedZIP struct {
	name   string
	folder int
	offset uint64
	size   uint64
}

func inspect7zNames(index []byte) (catalog7z, error) {
	var catalog catalog7z
	c := cursor7z{data: index}
	c.expect(idHeader)
	if c.match(idProperties) {
		for c.err == nil && !c.match(idEnd) {
			c.byte()
			c.take(c.number())
		}
	}
	if c.match(idAdditional) {
		// This can contain externally stored names. Do not read those streams.
		return catalog7z{}, errUnsupported7z
	}
	if c.match(idMain) {
		catalog.streams = read7zStreams(&c)
	}
	if c.match(idFiles) {
		read7zFiles(&c, &catalog)
	}
	c.expect(idEnd)
	if c.err != nil {
		return catalog7z{}, c.err
	}
	if len(c.data) != 0 {
		return catalog7z{}, errInvalid7z
	}
	return catalog, nil
}

func read7zFiles(c *cursor7z, catalog *catalog7z) {
	files := c.count(maxIndexEntries)
	var names, empty, emptyFiles, anti []byte
	var attributes []uint32
	for c.err == nil && !c.match(idEnd) {
		id := c.byte()
		data := c.take(c.number())
		switch id {
		case idName:
			if len(data) == 0 {
				c.fail(errInvalid7z)
			} else if data[0] != 0 {
				c.fail(errUnsupported7z)
			} else {
				names = data[1:]
			}
		case idEmpty:
			empty = data
		case idEmptyFile:
			emptyFiles = data
		case idAnti:
			anti = data
		case idAttributes:
			attributes = read7zAttributes(c, data, files)
		}
	}
	emptyCount := uint64(0)
	if empty != nil && uint64(len(empty)) != (files+7)/8 {
		c.fail(errInvalid7z)
		return
	}
	for i := uint64(0); i < files; i++ {
		if bit7z(empty, i) {
			emptyCount++
		}
	}
	var streams uint64
	for _, folder := range catalog.streams.folders {
		streams += folder.streams
	}
	if streams != files-emptyCount ||
		(emptyFiles != nil && uint64(len(emptyFiles)) != (emptyCount+7)/8) ||
		(anti != nil && uint64(len(anti)) != (emptyCount+7)/8) {
		c.fail(errInvalid7z)
		return
	}

	n := cursor7z{data: names}
	streamsAt := fileStreams7z{folders: catalog.streams.folders}
	emptyIndex := uint64(0)
	for i := uint64(0); i < files && n.err == nil; i++ {
		name := n.name()
		regular := true
		entry := nestedZIP{name: name, folder: -1}
		if bit7z(empty, i) {
			regular = bit7z(emptyFiles, emptyIndex) && !bit7z(anti, emptyIndex)
			emptyIndex++
		} else {
			entry = streamsAt.next(name)
		}
		if attributes != nil {
			const directory = 0x10
			const unixType = 0o170000
			const unixRegular = 0o100000
			kind := attributes[i] >> 16 & unixType
			regular = regular && attributes[i]&directory == 0 && (kind == 0 || kind == unixRegular)
		}
		if !regular || strings.HasSuffix(name, "/") || strings.HasSuffix(name, "\\") {
			continue
		}
		extension := strings.ToLower(path.Ext(name))
		if knownROMExtension(extension) {
			catalog.formats.add(extension)
		} else if extension == ".zip" {
			catalog.zips = append(catalog.zips, entry)
		}
	}
	if n.err != nil || len(n.data) != 0 {
		c.fail(errInvalid7z)
	}
}

func read7zAttributes(parent *cursor7z, data []byte, files uint64) []uint32 {
	c := cursor7z{data: data}
	all := c.byte() != 0
	var defined []byte
	if !all {
		defined = c.take((files + 7) / 8)
	}
	if c.byte() != 0 {
		parent.fail(errUnsupported7z)
		return nil
	}
	attributes := make([]uint32, files)
	for i := uint64(0); i < files && c.err == nil; i++ {
		if all || bit7z(defined, i) {
			if value := c.take(4); value != nil {
				attributes[i] = binary.LittleEndian.Uint32(value)
			}
		}
	}
	if c.err != nil || len(c.data) != 0 {
		parent.fail(errInvalid7z)
	}
	return attributes
}
