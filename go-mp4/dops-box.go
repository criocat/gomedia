package mp4

import (
	"encoding/binary"
	"errors"
	"io"
)

// class ChannelMappingTable (unsigned int(8) OutputChannelCount){
//     unsigned int(8) StreamCount;
//     unsigned int(8) CoupledCount;
//     unsigned int(8 * OutputChannelCount) ChannelMapping;
// }
// aligned(8) class OpusSpecificBox extends Box('dOps'){
//     unsigned int(8) Version;
//     unsigned int(8) OutputChannelCount;
//     unsigned int(16) PreSkip;
//     unsigned int(32) InputSampleRate;
//     signed int(16) OutputGain;
//     unsigned int(8) ChannelMappingFamily;
//     if (ChannelMappingFamily != 0) {
//         ChannelMappingTable(OutputChannelCount);
//     }
// }

type ChannelMappingTable struct {
	StreamCount    uint8
	CoupledCount   uint8
	ChannelMapping []byte
}

type OpusSpecificBox struct {
	Box                *BasicBox
	Version            uint8
	OutputChannelCount uint8
	PreSkip            uint16
	InputSampleRate    uint32
	OutputGain         int16
	ChanMapTable       *ChannelMappingTable
}

func NewdOpsBox() *OpusSpecificBox {
	return &OpusSpecificBox{
		Box: NewBasicBox([4]byte{'d', 'O', 'p', 's'}),
	}
}

func (dops *OpusSpecificBox) Size() uint64 {
	size := uint64(BasicBoxLen + 11)
	if dops.ChanMapTable != nil {
		size += 2 + uint64(dops.OutputChannelCount)
	}
	return size
}

func (dops *OpusSpecificBox) Encode() (int, []byte) {
	family := uint8(0)
	if dops.ChanMapTable != nil {
		family = 1
	}
	return dops.EncodeWithChannelMappingFamily(family)
}

// EncodeWithChannelMappingFamily writes the specified mapping family.
func (dops *OpusSpecificBox) EncodeWithChannelMappingFamily(family uint8) (int, []byte) {
	table := dops.ChanMapTable
	if (family == 0) != (table == nil) {
		return 0, nil
	}
	if table != nil && len(table.ChannelMapping) != int(dops.OutputChannelCount) {
		return 0, nil
	}
	dops.Box.Size = dops.Size()
	offset, buf := dops.Box.Encode()
	buf[offset] = dops.Version
	offset++
	buf[offset] = dops.OutputChannelCount
	offset++
	binary.BigEndian.PutUint16(buf[offset:], dops.PreSkip)
	offset += 2
	binary.BigEndian.PutUint32(buf[offset:], dops.InputSampleRate)
	offset += 4
	binary.BigEndian.PutUint16(buf[offset:], uint16(dops.OutputGain))
	offset += 2
	buf[offset] = family
	offset++
	if table != nil {
		buf[offset] = table.StreamCount
		buf[offset+1] = table.CoupledCount
		offset += 2 + copy(buf[offset+2:], table.ChannelMapping)
	}
	return offset, buf
}

func (dops *OpusSpecificBox) Decode(r io.Reader, size uint32) (int, error) {
	n, _, err := dops.DecodeWithChannelMappingFamily(r, size)
	return n, err
}

// DecodeWithChannelMappingFamily reads the box and returns its mapping family
// alongside the number of bytes read.
func (dops *OpusSpecificBox) DecodeWithChannelMappingFamily(r io.Reader, size uint32) (int, uint8, error) {
	var fields [11]byte
	if size < BasicBoxLen+uint32(len(fields)) {
		return 0, 0, io.ErrUnexpectedEOF
	}
	if n, err := io.ReadFull(r, fields[:]); err != nil {
		return n, 0, err
	}
	if fields[0] != 0 {
		return len(fields), 0, errors.New("unsupported dOps version")
	}
	family := fields[10]
	mappingSize := 0
	if family != 0 {
		mappingSize = 2 + int(fields[1])
	}
	if size != uint32(BasicBoxLen+len(fields)+mappingSize) {
		return len(fields), 0, errors.New("invalid dOps box size for channel mapping family")
	}
	var table *ChannelMappingTable
	if mappingSize > 0 {
		data := make([]byte, mappingSize)
		if n, err := io.ReadFull(r, data); err != nil {
			return len(fields) + n, 0, err
		}
		table = &ChannelMappingTable{StreamCount: data[0], CoupledCount: data[1], ChannelMapping: data[2:]}
	}

	dops.Version = fields[0]
	dops.OutputChannelCount = fields[1]
	dops.PreSkip = binary.BigEndian.Uint16(fields[2:])
	dops.InputSampleRate = binary.BigEndian.Uint32(fields[4:])
	dops.OutputGain = int16(binary.BigEndian.Uint16(fields[8:]))
	dops.ChanMapTable = table
	return len(fields) + mappingSize, family, nil
}

func opusSpecificBoxFromHead(extraData []byte) (*OpusSpecificBox, uint8, error) {
	if len(extraData) < 19 || string(extraData[:8]) != "OpusHead" || extraData[9] == 0 {
		return nil, 0, errors.New("invalid OpusHead")
	}
	dops := NewdOpsBox()
	dops.OutputChannelCount = extraData[9]
	dops.PreSkip = binary.LittleEndian.Uint16(extraData[10:])
	dops.InputSampleRate = binary.LittleEndian.Uint32(extraData[12:])
	dops.OutputGain = int16(binary.LittleEndian.Uint16(extraData[16:]))
	family := extraData[18]
	if family == 0 && dops.OutputChannelCount > 2 {
		return nil, 0, errors.New("OpusHead with more than two channels requires a channel mapping")
	}
	if family != 0 {
		end := 21 + int(dops.OutputChannelCount)
		if len(extraData) < end {
			return nil, 0, errors.New("incomplete OpusHead channel mapping")
		}
		dops.ChanMapTable = &ChannelMappingTable{
			StreamCount:    extraData[19],
			CoupledCount:   extraData[20],
			ChannelMapping: extraData[21:end],
		}
	}
	return dops, family, nil
}

func decodeOpusSampleEntry(demuxer *MovDemuxer, format [4]byte, size uint64) error {
	if len(demuxer.tracks) == 0 || size > uint64(1<<63-1) {
		return errors.New("invalid Opus sample entry")
	}
	reader := &io.LimitedReader{R: demuxer.reader, N: int64(size)}
	entry := NewAudioSampleEntry(format)
	if _, err := entry.Decode(reader); err != nil {
		return err
	}
	channels := uint8(entry.channelcount)
	for reader.N > 0 {
		var box BasicBox
		headerSize, err := box.Decode(reader)
		if err != nil {
			return err
		}
		if box.Size < uint64(headerSize) || box.Size-uint64(headerSize) > uint64(reader.N) {
			return errors.New("invalid Opus sample entry child size")
		}
		bodySize := box.Size - uint64(headerSize)
		if box.Type == [4]byte{'d', 'O', 'p', 's'} {
			if format[0] == 'O' {
				if bodySize > 11+2+255 {
					return errors.New("dOps box too large")
				}
				config := NewdOpsBox()
				if _, err := config.Decode(reader, uint32(bodySize)+BasicBoxLen); err != nil {
					return err
				}
				channels = config.OutputChannelCount
				continue
			}
			// Old gomedia files put the output count in the same position,
			// but used a different size and layout for the rest of dOps.
			if bodySize >= 2 {
				var fields [2]byte
				if _, err := io.ReadFull(reader, fields[:]); err != nil {
					return err
				}
				if fields[1] != 0 {
					channels = fields[1]
				}
				bodySize -= 2
			}
		}
		if _, err := io.CopyN(io.Discard, reader, int64(bodySize)); err != nil {
			return err
		}
	}
	track := demuxer.tracks[len(demuxer.tracks)-1]
	track.cid = MP4_CODEC_OPUS
	track.chanelCount = channels
	track.sampleBits = uint8(entry.samplesize)
	track.sampleRate = entry.samplerate
	return nil
}
