package mp4
import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	UseSubsampleEncryption uint32 = 0x000002
)

// SencBox - Sample Encryption Box (senc) (in trak or traf box)
// See ISO/IEC 23001-7 Section 7.2 and CMAF specification
// Full Box + SampleCount
type SencBox struct{
	Box              *FullBox
	SampleCount      uint32
	PerSampleIVSize  uint32
	EntryList		 *movsenc
}

func (senc *SencBox) Decode(r io.Reader, size uint32, perSampleIVSize uint8) (offset int, err error) {
	offset, buf, err := senc.Box.decodeCountedPayload(r, size)
	if err != nil {
		return offset, err
	}
	senc.PerSampleIVSize = uint32(perSampleIVSize)
	n := 0
	senc.SampleCount = binary.BigEndian.Uint32(buf[n:])
	n += 4
	sencFlags := uint32(senc.Box.Flags[0])<<16 | uint32(senc.Box.Flags[1])<<8 | uint32(senc.Box.Flags[2])
	ivSize := int(perSampleIVSize)
	minEntrySize := ivSize
	hasSubsamples := sencFlags&UseSubsampleEncryption != 0
	if hasSubsamples {
		minEntrySize += 2
	}
	if uint64(minEntrySize)*uint64(senc.SampleCount) > uint64(len(buf)-n) {
		return 0, io.ErrUnexpectedEOF
	}

	senc.EntryList = new(movsenc)
	senc.EntryList.entrys = make([]sencEntry, senc.SampleCount)
	for i := 0; i < int(senc.SampleCount); i++ {
		if len(buf)-n < minEntrySize {
			return 0, io.ErrUnexpectedEOF
		}
		senc.EntryList.entrys[i].iv = buf[n:n+ivSize]
		n += ivSize

		if !hasSubsamples {
			continue
		}

		subsampleCount := binary.BigEndian.Uint16(buf[n:])
		n += 2
		if int(subsampleCount) > (len(buf)-n)/6 {
			return 0, io.ErrUnexpectedEOF
		}

		senc.EntryList.entrys[i].subSamples = make([]subSampleEntry, subsampleCount)
		for j := uint16(0); j < subsampleCount; j++ {
			senc.EntryList.entrys[i].subSamples[j].bytesOfClearData = binary.BigEndian.Uint16(buf[n:])
			n += 2
			senc.EntryList.entrys[i].subSamples[j].bytesOfProtectedData = binary.BigEndian.Uint32(buf[n:])
			n += 4
		}
	}

	offset += n
	return
}

func decodeSencBox(demuxer *MovDemuxer, size uint32) (err error) {
	track := demuxer.currentTrack
	if track == nil {
		return errors.New("senc box has no track")
	}
	senc := SencBox{Box: new(FullBox)}
	if _, err = senc.Decode(demuxer.reader, size, track.defaultPerSampleIVSize); err != nil {
		return err
	}
	track.subSamples = append(track.subSamples, senc.EntryList.entrys...)
	return
}

func (demuxer *MovDemuxer) sampleEncryption(track *mp4track, index uint32) *SubSample {
	if uint64(index) >= uint64(len(track.subSamples)) {
		return nil
	}
	entry := track.subSamples[index]
	sample := &SubSample{Number: index}
	if len(entry.iv) > 0 {
		copy(sample.IV[:], entry.iv)
	} else {
		copy(sample.IV[:], track.defaultConstantIV)
	}
	if track.lastSeig != nil {
		sample.KID = track.lastSeig.KID
		sample.CryptByteBlock = track.lastSeig.CryptByteBlock
		sample.SkipByteBlock = track.lastSeig.SkipByteBlock
	} else {
		sample.KID = track.defaultKID
		sample.CryptByteBlock = track.defaultCryptByteBlock
		sample.SkipByteBlock = track.defaultSkipByteBlock
	}
	sample.PsshBoxes = append(sample.PsshBoxes, demuxer.pssh...)
	for _, pattern := range entry.subSamples {
		sample.Patterns = append(sample.Patterns, SubSamplePattern{
			BytesClear: pattern.bytesOfClearData, BytesProtected: pattern.bytesOfProtectedData,
		})
	}
	return sample
}
