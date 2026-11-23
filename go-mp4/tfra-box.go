package mp4

import (
    "encoding/binary"
    "io"
    "math/bits"
)

// aligned(8) class TrackFragmentRandomAccessBox
// extends FullBox(‘tfra’, version, 0) {
// 	unsigned int(32)  track_ID;
// 	const unsigned int(26)  reserved = 0;
// 	unsigned int(2) length_size_of_traf_num;
// 	unsigned int(2) length_size_of_trun_num;
// 	unsigned int(2)  length_size_of_sample_num;
// 	unsigned int(32)  number_of_entry;
// 	for(i=1; i <= number_of_entry; i++){
// 		if(version==1){
// 			unsigned int(64)  time;
// 			unsigned int(64)  moof_offset;
// 		 }else{
// 			unsigned int(32)  time;
// 			unsigned int(32)  moof_offset;
// 		 }
// 		 unsignedint((length_size_of_traf_num+1)*8) traf_number;
// 		 unsignedint((length_size_of_trun_num+1)*8) trun_number;
// 		 unsigned int((length_size_of_sample_num+1) * 8)sample_number;
// 	}
// }

type TrackFragmentRandomAccessBox struct {
    Box                   *FullBox
    TrackID               uint32
    LengthSizeOfTrafNum   uint8
    LengthSizeOfTrunNum   uint8
    LengthSizeOfSampleNum uint8
    NumberOfEntry         uint32
    FragEntrys            *movtfra
}

func NewTrackFragmentRandomAccessBox(trackid uint32) *TrackFragmentRandomAccessBox {
    return &TrackFragmentRandomAccessBox{
        Box:     NewFullBox([4]byte{'t', 'f', 'r', 'a'}, 1),
        TrackID: trackid,
    }
}

func (tfra *TrackFragmentRandomAccessBox) Size() uint64 {
    return FullBoxLen + 12 + uint64(tfra.NumberOfEntry)*uint64(tfra.entrySize())
}

func (tfra *TrackFragmentRandomAccessBox) entrySize() int {
    stampSize := 8
    if tfra.Box.Version == 1 {
        stampSize = 16
    }
    return stampSize + 3 + int(tfra.LengthSizeOfTrafNum+tfra.LengthSizeOfTrunNum+tfra.LengthSizeOfSampleNum)
}

func readTfraNumber(data []byte) uint32 {
    var number uint32
    for _, octet := range data {
        number = number<<8 | uint32(octet)
    }
    return number
}

func writeTfraNumber(data []byte, number uint32, width int) int {
    var encoded [4]byte
    binary.BigEndian.PutUint32(encoded[:], number)
    return copy(data, encoded[len(encoded)-width:])
}

func (tfra *TrackFragmentRandomAccessBox) Decode(r io.Reader) (offset int, err error) {
    if offset, err = tfra.Box.Decode(r); err != nil {
        return
    }

    if tfra.Box.Box.Size < FullBoxLen+12 {
        return 0, io.ErrUnexpectedEOF
    }
    needSize := tfra.Box.Box.Size - FullBoxLen
    buf := make([]byte, needSize)
    if _, err = io.ReadFull(r, buf); err != nil {
        return 0, err
    }
    n := 0
    tfra.TrackID = binary.BigEndian.Uint32(buf[n:])
    n += 4
    tfra.LengthSizeOfTrafNum = (buf[n+3] >> 4) & 0x03
    tfra.LengthSizeOfTrunNum = (buf[n+3] >> 2) & 0x03
    tfra.LengthSizeOfSampleNum = buf[n+3] & 0x03
    n += 4
    tfra.NumberOfEntry = binary.BigEndian.Uint32(buf[n:])
    n += 4
    if uint64(tfra.NumberOfEntry) > uint64((len(buf)-n)/tfra.entrySize()) {
        return 0, io.ErrUnexpectedEOF
    }
    trafWidth := int(tfra.LengthSizeOfTrafNum) + 1
    trunWidth := int(tfra.LengthSizeOfTrunNum) + 1
    sampleWidth := int(tfra.LengthSizeOfSampleNum) + 1
    tfra.FragEntrys = new(movtfra)
    tfra.FragEntrys.frags = make([]fragEntry, tfra.NumberOfEntry)
    for i := 0; i < int(tfra.NumberOfEntry); i++ {
        entry := &tfra.FragEntrys.frags[i]
        if tfra.Box.Version == 1 {
            entry.time = binary.BigEndian.Uint64(buf[n:])
            n += 8
            entry.moofOffset = binary.BigEndian.Uint64(buf[n:])
            n += 8
        } else {
            entry.time = uint64(binary.BigEndian.Uint32(buf[n:]))
            n += 4
            entry.moofOffset = uint64(binary.BigEndian.Uint32(buf[n:]))
            n += 4
        }
        entry.trafNumber = readTfraNumber(buf[n : n+trafWidth])
        n += trafWidth
        entry.trunNumber = readTfraNumber(buf[n : n+trunWidth])
        n += trunWidth
        entry.sampleNumber = readTfraNumber(buf[n : n+sampleWidth])
        n += sampleWidth
    }
    return offset + len(buf), nil
}

func (tfra *TrackFragmentRandomAccessBox) Encode() (int, []byte) {
    tfra.Box.Box.Size = tfra.Size()
    offset, boxdata := tfra.Box.Encode()
    binary.BigEndian.PutUint32(boxdata[offset:], tfra.TrackID)
    offset += 4
    widths := uint32(tfra.LengthSizeOfTrafNum)<<4 |
        uint32(tfra.LengthSizeOfTrunNum)<<2 | uint32(tfra.LengthSizeOfSampleNum)
    binary.BigEndian.PutUint32(boxdata[offset:], widths)
    offset += 4
    binary.BigEndian.PutUint32(boxdata[offset:], tfra.NumberOfEntry)
    offset += 4
    for i := 0; i < int(tfra.NumberOfEntry); i++ {
        if tfra.Box.Version == 1 {
            binary.BigEndian.PutUint64(boxdata[offset:], tfra.FragEntrys.frags[i].time)
            offset += 8
            binary.BigEndian.PutUint64(boxdata[offset:], tfra.FragEntrys.frags[i].moofOffset)
            offset += 8
        } else {
            binary.BigEndian.PutUint32(boxdata[offset:], uint32(tfra.FragEntrys.frags[i].time))
            offset += 4
            binary.BigEndian.PutUint32(boxdata[offset:], uint32(tfra.FragEntrys.frags[i].moofOffset))
            offset += 4
        }
        entry := tfra.FragEntrys.frags[i]
        offset += writeTfraNumber(boxdata[offset:], entry.trafNumber, int(tfra.LengthSizeOfTrafNum)+1)
        offset += writeTfraNumber(boxdata[offset:], entry.trunNumber, int(tfra.LengthSizeOfTrunNum)+1)
        offset += writeTfraNumber(boxdata[offset:], entry.sampleNumber, int(tfra.LengthSizeOfSampleNum)+1)
    }
    return offset, boxdata
}

func makeTfraBox(track *mp4track) []byte {
    tfra := NewTrackFragmentRandomAccessBox(track.trackId)
    tfra.NumberOfEntry = uint32(len(track.fragments))
    frags := make([]fragEntry, 0, len(track.fragments))
    var maxTrafNumber uint32
    for _, fragment := range track.fragments {
        frags = append(frags, fragEntry{
            time:         fragment.firstPts,
            moofOffset:   fragment.offset,
            trafNumber:   fragment.trafNumber,
            trunNumber:   1,
            sampleNumber: 1,
        })
        if fragment.trafNumber > maxTrafNumber {
            maxTrafNumber = fragment.trafNumber
        }
    }
    if maxTrafNumber != 0 {
        tfra.LengthSizeOfTrafNum = uint8((bits.Len32(maxTrafNumber) - 1) / 8)
    }
    entrys := &movtfra{
        frags: frags,
    }
    tfra.FragEntrys = entrys
    _, tfraData := tfra.Encode()
    return tfraData
}
