package flv

import (
	"encoding/binary"
	"errors"

	"github.com/yapingcat/gomedia/go-codec"
)

// nalu is supplied by SplitFrameWithStartCode. Append its length and payload
// without replacing the start code in the caller's buffer.
func appendHEVCNAL(dst, nalu []byte) []byte {
	start, sc := codec.FindStartCode(nalu, 0)
	payload := nalu[start+int(sc):]
	size := uint32(len(payload))
	dst = append(dst, byte(size>>24), byte(size>>16), byte(size>>8), byte(size))
	return append(dst, payload...)
}

func encodeHEVCVideoHeader(vtag VideoTag) []byte {
	packetType := vtag.AVCPacketType
	if packetType == PacketTypeCodedFrames && vtag.CompositionTime == 0 {
		packetType = PacketTypeCodedFramesX
	}
	tag := []byte{0x80 | (vtag.FrameType << 4) | packetType, 'h', 'v', 'c', '1'}
	if packetType == PacketTypeCodedFrames {
		tag = append(tag, 0, 0, 0)
		PutUint24(tag[5:], uint32(vtag.CompositionTime))
	}
	return tag
}

func hasHEVCParameterSets(hvcc *codec.HEVCRecordConfiguration) bool {
	var vps, sps, pps bool
	for _, array := range hvcc.Arrays {
		if len(array.NalUnits) == 0 {
			continue
		}
		switch array.NAL_unit_type {
		case 32:
			vps = true
		case 33:
			sps = true
		case 34:
			pps = true
		}
	}
	return vps && sps && pps
}

func (demuxer *HevcTagDemuxer) decodeHEVCTag(data []byte) error {
	if len(data) < 5 {
		return errors.New("hevc tag size < 5")
	}
	var packetType byte
	var cts int32
	headerSize := 5
	enhanced := data[0]&0x80 != 0
	if enhanced {
		if string(data[1:5]) != "hvc1" {
			return errors.New("unsupported enhanced HEVC FourCC")
		}
		packetType = data[0] & 0x0f
		if packetType == PacketTypeCodedFrames {
			if len(data) < 8 {
				return errors.New("truncated HEVC composition time")
			}
			cts = signedCompositionTime(data[5:8])
			headerSize = 8
		}
	} else {
		if data[0]&0x0f != byte(FLV_HEVC) {
			return errors.New("unexpected HEVC codec ID")
		}
		packetType = data[1]
		cts = signedCompositionTime(data[2:5])
		if packetType > PacketTypeSequenceEnd {
			return errors.New("unsupported legacy HEVC packet type")
		}
	}
	payload := data[headerSize:]
	switch packetType {
	case PacketTypeSequenceStart:
		return demuxer.decodeHEVCConfiguration(payload)
	case PacketTypeCodedFrames, PacketTypeCodedFramesX:
		return demuxer.decodeNalus(payload, cts, enhanced)
	case PacketTypeSequenceEnd:
		demuxer.SpsPpsVps = nil
		demuxer.naluLengthSize = 0
		return nil
	case PacketTypeMetadata:
		// Skip colorInfo metadata.
		return nil
	default:
		return errors.New("unsupported HEVC packet type")
	}
}

// Validate lengths before calling the bitstream-based hvcC decoder.
// Update the active configuration.
func (demuxer *HevcTagDemuxer) decodeHEVCConfiguration(data []byte) error {
	if len(data) < 23 || data[0] != 1 {
		return errors.New("invalid HEVC decoder configuration")
	}
	lengthSize := int(data[21]&3) + 1
	if lengthSize == 3 {
		return errors.New("reserved HEVC NAL length size")
	}
	rest := data[23:]
	for i := 0; i < int(data[22]); i++ {
		if len(rest) < 3 {
			return errors.New("truncated HEVC configuration array")
		}
		count := int(binary.BigEndian.Uint16(rest[1:3]))
		rest = rest[3:]
		for j := 0; j < count; j++ {
			if len(rest) < 2 {
				return errors.New("truncated HEVC configuration NAL length")
			}
			size := int(binary.BigEndian.Uint16(rest[:2]))
			rest = rest[2:]
			if size < 2 || size > len(rest) {
				return errors.New("invalid HEVC configuration NAL length")
			}
			rest = rest[size:]
		}
	}
	if len(rest) != 0 {
		return errors.New("trailing HEVC configuration data")
	}
	hvcc := codec.NewHEVCRecordConfiguration()
	hvcc.Decode(data)
	demuxer.SpsPpsVps = hvcc.ToNalus()
	demuxer.naluLengthSize = lengthSize
	return nil
}

func (demuxer *HevcTagDemuxer) decodeNalus(data []byte, cts int32, enhanced bool) error {
	lengthSize := demuxer.naluLengthSize
	legacyStartup := !enhanced && lengthSize == 0
	if legacyStartup {
		lengthSize = 4
	}
	if lengthSize == 0 {
		return errors.New("HEVC frame before decoder configuration")
	}
	if len(data) == 0 {
		return errors.New("empty HEVC frame")
	}
	var frame []byte
	var parameterSets []byte
	var irap, vps, sps, pps bool
	for len(data) > 0 {
		if len(data) < lengthSize {
			return errors.New("truncated HEVC NAL length")
		}
		var size uint32
		for _, b := range data[:lengthSize] {
			size = size<<8 | uint32(b)
		}
		data = data[lengthSize:]
		if size < 2 || uint64(size) > uint64(len(data)) {
			return errors.New("invalid HEVC NAL length")
		}
		nalType := codec.H265NaluTypeWithoutStartCode(data)
		irap = irap || (nalType >= 16 && nalType <= 21)
		vps = vps || nalType == codec.H265_NAL_VPS
		sps = sps || nalType == codec.H265_NAL_SPS
		pps = pps || nalType == codec.H265_NAL_PPS
		offset := len(frame)
		frame = append(frame, 0, 0, 0, 1)
		frame = append(frame, data[:size]...)
		if legacyStartup && nalType >= codec.H265_NAL_VPS && nalType <= codec.H265_NAL_PPS {
			parameterSets = append(parameterSets, frame[offset:]...)
		}
		data = data[size:]
	}
	if legacyStartup {
		if !irap || !vps || !sps || !pps {
			return errors.New("legacy HEVC startup requires a keyframe with VPS, SPS and PPS")
		}
		demuxer.SpsPpsVps = parameterSets
		demuxer.naluLengthSize = lengthSize
	} else if irap && (!vps || !sps || !pps) {
		// Allocate a separate frame, queued frames must not alias cached VPS/SPS/PPS.
		prefixed := make([]byte, 0, len(demuxer.SpsPpsVps)+len(frame))
		prefixed = append(prefixed, demuxer.SpsPpsVps...)
		frame = append(prefixed, frame...)
	}
	if demuxer.onframe != nil {
		demuxer.onframe(codec.CODECID_VIDEO_H265, frame, int(cts))
	}
	return nil
}
