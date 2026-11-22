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
	Box                  *BasicBox
	Version              uint8
	OutputChannelCount   uint8
	PreSkip              uint16
	InputSampleRate      uint32
	OutputGain           int16
	ChannelMappingFamily uint8
	ChanMapTable         *ChannelMappingTable
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
	family := dops.ChannelMappingFamily
	table := dops.ChanMapTable
	if table != nil {
		if len(table.ChannelMapping) != int(dops.OutputChannelCount) {
			return 0, nil
		}
		if family == 0 {
			family = 1 // Default for callers that supply a table without a family.
		}
	} else if family != 0 {
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
	var fields [11]byte
	if size < BasicBoxLen+uint32(len(fields)) {
		return 0, io.ErrUnexpectedEOF
	}
	if n, err := io.ReadFull(r, fields[:]); err != nil {
		return n, err
	}
	if fields[0] != 0 {
		return len(fields), errors.New("unsupported dOps version")
	}
	family := fields[10]
	mappingSize := 0
	if family != 0 {
		mappingSize = 2 + int(fields[1])
	}
	if size != uint32(BasicBoxLen+len(fields)+mappingSize) {
		return len(fields), errors.New("invalid dOps box size for channel mapping family")
	}
	var table *ChannelMappingTable
	if mappingSize > 0 {
		data := make([]byte, mappingSize)
		if n, err := io.ReadFull(r, data); err != nil {
			return len(fields) + n, err
		}
		table = &ChannelMappingTable{StreamCount: data[0], CoupledCount: data[1], ChannelMapping: data[2:]}
	}

	dops.Version = fields[0]
	dops.OutputChannelCount = fields[1]
	dops.PreSkip = binary.BigEndian.Uint16(fields[2:])
	dops.InputSampleRate = binary.BigEndian.Uint32(fields[4:])
	dops.OutputGain = int16(binary.BigEndian.Uint16(fields[8:]))
	dops.ChannelMappingFamily = family
	dops.ChanMapTable = table
	return len(fields) + mappingSize, nil
}

func opusSpecificBoxFromHead(extraData []byte) (*OpusSpecificBox, error) {
	if len(extraData) < 19 || string(extraData[:8]) != "OpusHead" || extraData[9] == 0 {
		return nil, errors.New("invalid OpusHead")
	}
	dops := NewdOpsBox()
	dops.OutputChannelCount = extraData[9]
	dops.PreSkip = binary.LittleEndian.Uint16(extraData[10:])
	dops.InputSampleRate = binary.LittleEndian.Uint32(extraData[12:])
	dops.OutputGain = int16(binary.LittleEndian.Uint16(extraData[16:]))
	dops.ChannelMappingFamily = extraData[18]
	if dops.ChannelMappingFamily != 0 {
		end := 21 + int(dops.OutputChannelCount)
		if len(extraData) < end {
			return nil, errors.New("incomplete OpusHead channel mapping")
		}
		dops.ChanMapTable = &ChannelMappingTable{
			StreamCount:    extraData[19],
			CoupledCount:   extraData[20],
			ChannelMapping: extraData[21:end],
		}
	}
	return dops, nil
}

func makeOpusSpecificBox(extraData []byte) []byte {
	dops, err := opusSpecificBoxFromHead(extraData)
	if err != nil {
		return nil
	}
	_, dopsbox := dops.Encode()
	return dopsbox
}
