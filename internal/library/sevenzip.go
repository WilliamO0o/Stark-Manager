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
// decoder dictionary independently. No reader is created for file payloads.
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

func sevenZipHasSNESROM(root fs.FS, name string) (bool, error) {
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
		return false, errUnsupported7z
	}
	return inspect7z(reader, info.Size())
}

func inspect7z(reader io.ReaderAt, size int64) (bool, error) {
	var signature [sevenZipHeaderSize]byte
	if _, err := reader.ReadAt(signature[:], 0); err != nil {
		return false, err
	}
	if string(signature[:6]) != "7z\xbc\xaf\x27\x1c" || signature[6] != 0 ||
		crc32.ChecksumIEEE(signature[12:]) != binary.LittleEndian.Uint32(signature[8:12]) {
		return false, errInvalid7z
	}
	offset := binary.LittleEndian.Uint64(signature[12:20])
	length := binary.LittleEndian.Uint64(signature[20:28])
	if length > maxIndexBytes {
		return false, errLimited7z
	}
	if size < sevenZipHeaderSize || offset > uint64(size-sevenZipHeaderSize) ||
		length > uint64(size-sevenZipHeaderSize)-offset {
		return false, errInvalid7z
	}
	if length == 0 && binary.LittleEndian.Uint32(signature[28:]) == 0 {
		return false, nil // A valid empty archive has no next header.
	}
	index := make([]byte, int(length))
	if _, err := reader.ReadAt(index, sevenZipHeaderSize+int64(offset)); err != nil {
		return false, err
	}
	if crc32.ChecksumIEEE(index) != binary.LittleEndian.Uint32(signature[28:]) {
		return false, errInvalid7z
	}
	if len(index) == 0 {
		return false, errInvalid7z
	}
	if index[0] == idEncoded {
		var err error
		index, err = decode7zIndex(reader, offset, index[1:])
		if err != nil {
			return false, err
		}
	}
	return inspect7zNames(index)
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
	var stream io.Reader
	var err error
	switch codec.method {
	case "\x00": // Copy
		stream = bytes.NewReader(packed)
	case "\x03\x01\x01": // LZMA
		if len(codec.properties) != 5 {
			return nil, errInvalid7z
		}
		if binary.LittleEndian.Uint32(codec.properties[1:]) > maxDictionary {
			return nil, errLimited7z
		}
		var header [13]byte
		copy(header[:], codec.properties)
		binary.LittleEndian.PutUint64(header[5:], size)
		stream, err = lzma.ReaderConfig{DictCap: maxDictionary}.NewReader(io.MultiReader(bytes.NewReader(header[:]), bytes.NewReader(packed)))
	case "\x21": // LZMA2
		if len(codec.properties) != 1 || codec.properties[0] > 40 {
			return nil, errInvalid7z
		}
		prop := codec.properties[0]
		dictionary := uint64(2|prop&1) << (prop/2 + 11)
		if dictionary > maxDictionary {
			return nil, errLimited7z
		}
		stream, err = lzma.Reader2Config{DictCap: int(dictionary)}.NewReader2(bytes.NewReader(packed))
	default:
		return nil, errUnsupported7z
	}
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

func inspect7zNames(index []byte) (bool, error) {
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
		return false, errUnsupported7z
	}
	var streamCount uint64
	if c.match(idMain) {
		streams := read7zStreams(&c)
		for _, folder := range streams.folders {
			streamCount += folder.streams
		}
	}
	var found bool
	if c.match(idFiles) {
		found = read7zFiles(&c, streamCount)
	}
	c.expect(idEnd)
	if c.err != nil {
		return false, c.err
	}
	if len(c.data) != 0 {
		return false, errInvalid7z
	}
	return found, nil
}

func read7zFiles(c *cursor7z, streams uint64) bool {
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
		return false
	}
	for i := uint64(0); i < files; i++ {
		if bit7z(empty, i) {
			emptyCount++
		}
	}
	if streams != files-emptyCount ||
		(emptyFiles != nil && uint64(len(emptyFiles)) != (emptyCount+7)/8) ||
		(anti != nil && uint64(len(anti)) != (emptyCount+7)/8) {
		c.fail(errInvalid7z)
		return false
	}

	n := cursor7z{data: names}
	found := false
	emptyIndex := uint64(0)
	for i := uint64(0); i < files && n.err == nil; i++ {
		name := n.name()
		regular := true
		if bit7z(empty, i) {
			regular = bit7z(emptyFiles, emptyIndex) && !bit7z(anti, emptyIndex)
			emptyIndex++
		}
		if attributes != nil {
			const directory = 0x10
			const unixType = 0o170000
			const unixRegular = 0o100000
			kind := attributes[i] >> 16 & unixType
			regular = regular && attributes[i]&directory == 0 && (kind == 0 || kind == unixRegular)
		}
		if regular && !strings.HasSuffix(name, "/") && !strings.HasSuffix(name, "\\") &&
			snesROMExtension(strings.ToLower(path.Ext(name))) {
			found = true
		}
	}
	if n.err != nil || len(n.data) != 0 {
		c.fail(errInvalid7z)
	}
	return found
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
