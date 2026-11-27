package library

import (
	"encoding/binary"
	"unicode/utf16"
)

type cursor7z struct {
	data []byte
	err  error
}

func (c *cursor7z) fail(err error) {
	if c.err == nil {
		c.err = err
	}
}

func (c *cursor7z) take(size uint64) []byte {
	if c.err != nil {
		return nil
	}
	if size > uint64(len(c.data)) {
		c.fail(errInvalid7z)
		return nil
	}
	value := c.data[:int(size)]
	c.data = c.data[int(size):]
	return value
}

func (c *cursor7z) byte() byte {
	if value := c.take(1); value != nil {
		return value[0]
	}
	return 0
}

func (c *cursor7z) match(id byte) bool {
	if c.err == nil && len(c.data) != 0 && c.data[0] == id {
		c.data = c.data[1:]
		return true
	}
	return false
}

func (c *cursor7z) expect(id byte) {
	if !c.match(id) {
		c.fail(errInvalid7z)
	}
}

func (c *cursor7z) number() uint64 {
	first := c.byte()
	var value uint64
	for i, mask := uint(0), byte(0x80); i < 8; i, mask = i+1, mask>>1 {
		if first&mask == 0 {
			return value | uint64(first&(mask-1))<<(8*i)
		}
		value |= uint64(c.byte()) << (8 * i)
	}
	return value
}

func (c *cursor7z) count(max uint64) uint64 {
	count := c.number()
	if count > max {
		c.fail(errLimited7z)
		return 0
	}
	return count
}

func (c *cursor7z) name() string {
	var units []uint16
	for c.err == nil {
		pair := c.take(2)
		if pair == nil {
			break
		}
		unit := binary.LittleEndian.Uint16(pair)
		if unit == 0 {
			return string(utf16.Decode(units))
		}
		units = append(units, unit)
	}
	return ""
}

func bit7z(data []byte, index uint64) bool {
	return index/8 < uint64(len(data)) && data[index/8]&(0x80>>(index%8)) != 0
}

type coder7z struct {
	method     string
	properties []byte
}

type folder7z struct {
	coders     []coder7z
	outputs    uint64
	final      uint64
	outputSize uint64
	packed     uint64
	streams    uint64
	firstPack  uint64
	subSizes   []uint64
	crc        *uint32
}

type streams7z struct {
	packOffset uint64
	packSizes  []uint64
	packCRC    *uint32
	folders    []folder7z
}

func read7zStreams(c *cursor7z) streams7z {
	var info streams7z
	if c.match(idPack) {
		info.packOffset = c.number()
		packs := c.count(maxIndexEntries)
		if c.match(idSize) {
			for i := uint64(0); i < packs && c.err == nil; i++ {
				info.packSizes = append(info.packSizes, c.number())
			}
		}
		if c.match(idCRC) {
			read7zDigests(c, packs, func(i uint64, crc uint32) {
				if i == 0 {
					info.packCRC = &crc
				}
			})
		}
		c.expect(idEnd)
	}
	if c.match(idUnpack) {
		c.expect(idFolder)
		count := c.count(maxIndexEntries)
		if c.byte() != 0 {
			c.fail(errUnsupported7z)
		}
		for i := uint64(0); i < count && c.err == nil; i++ {
			info.folders = append(info.folders, read7zFolder(c))
		}
		c.expect(idUnpackSize)
		packed := uint64(0)
		for i := range info.folders {
			folder := &info.folders[i]
			folder.firstPack = packed
			packed += folder.packed
			for j := uint64(0); j < folder.outputs && c.err == nil; j++ {
				size := c.number()
				if j == folder.final {
					folder.outputSize = size
				}
			}
		}
		if packed != uint64(len(info.packSizes)) {
			c.fail(errInvalid7z)
		}
		if c.match(idCRC) {
			read7zDigests(c, count, func(i uint64, crc uint32) { info.folders[i].crc = &crc })
		}
		c.expect(idEnd)
	}
	if c.match(idSubstreams) {
		read7zSubstreams(c, info.folders)
	}
	c.expect(idEnd)
	return info
}

func read7zFolder(c *cursor7z) folder7z {
	var folder folder7z
	folder.streams = 1
	coders := c.count(16)
	inputs := uint64(0)
	for i := uint64(0); i < coders && c.err == nil; i++ {
		flags := c.byte()
		if flags&0xc0 != 0 || flags&0x0f == 0 {
			c.fail(errUnsupported7z)
			break
		}
		coder := coder7z{method: string(c.take(uint64(flags & 0x0f)))}
		in, out := uint64(1), uint64(1)
		if flags&0x10 != 0 {
			in, out = c.count(64), c.count(64)
		}
		if flags&0x20 != 0 {
			coder.properties = c.take(c.count(4096))
		}
		if in == 0 || out == 0 {
			c.fail(errInvalid7z)
		}
		inputs += in
		folder.outputs += out
		folder.coders = append(folder.coders, coder)
	}
	if folder.outputs == 0 || inputs < folder.outputs || inputs > 64 || folder.outputs > 64 {
		c.fail(errInvalid7z)
		return folder
	}
	var inUsed, outUsed uint64
	for i := uint64(1); i < folder.outputs && c.err == nil; i++ {
		in, out := c.number(), c.number()
		if in >= inputs || out >= folder.outputs || inUsed&(1<<in) != 0 || outUsed&(1<<out) != 0 {
			c.fail(errInvalid7z)
			return folder
		}
		inUsed |= 1 << in
		outUsed |= 1 << out
	}
	for outUsed&(1<<folder.final) != 0 {
		folder.final++
	}
	folder.packed = inputs - (folder.outputs - 1)
	if folder.packed > 1 {
		for i := uint64(0); i < folder.packed && c.err == nil; i++ {
			in := c.number()
			if in >= inputs || inUsed&(1<<in) != 0 {
				c.fail(errInvalid7z)
				break
			}
			inUsed |= 1 << in
		}
	}
	return folder
}

func read7zSubstreams(c *cursor7z, folders []folder7z) {
	total := uint64(len(folders))
	if c.match(idNumStreams) {
		total = 0
		for i := range folders {
			folders[i].streams = c.count(maxIndexEntries)
			total += folders[i].streams
			if total > maxIndexEntries {
				c.fail(errLimited7z)
				return
			}
		}
	}
	hasSizes := c.match(idSize)
	for i := range folders {
		folder := &folders[i]
		remaining := folder.outputSize
		for j := uint64(1); j < folder.streams && c.err == nil; j++ {
			if !hasSizes {
				c.fail(errInvalid7z)
				return
			}
			size := c.number()
			if size > remaining {
				c.fail(errInvalid7z)
				return
			}
			folder.subSizes = append(folder.subSizes, size)
			remaining -= size
		}
		if folder.streams > 1 {
			folder.subSizes = append(folder.subSizes, remaining)
		}
	}
	if c.match(idCRC) {
		for _, folder := range folders {
			if folder.streams == 1 && folder.crc != nil {
				total--
			}
		}
		read7zDigests(c, total, func(uint64, uint32) {})
	}
	c.expect(idEnd)
}

func read7zDigests(c *cursor7z, count uint64, save func(uint64, uint32)) {
	all := c.byte() != 0
	var defined []byte
	if !all {
		defined = c.take((count + 7) / 8)
	}
	for i := uint64(0); i < count && c.err == nil; i++ {
		if all || bit7z(defined, i) {
			if crc := c.take(4); crc != nil {
				save(i, binary.LittleEndian.Uint32(crc))
			}
		}
	}
}

// 7z assigns each nonempty file to a substream in archive order.
type fileStreams7z struct {
	folders []folder7z
	folder  int
	index   uint64
	offset  uint64
}

func (s *fileStreams7z) next(name string) nestedZIP {
	for s.index == s.folders[s.folder].streams {
		s.folder++
		s.index, s.offset = 0, 0
	}
	folder := s.folders[s.folder]
	size := folder.outputSize
	if folder.streams > 1 {
		size = folder.subSizes[s.index]
	}
	entry := nestedZIP{name: name, folder: s.folder, offset: s.offset, size: size}
	s.index++
	s.offset += size
	return entry
}
