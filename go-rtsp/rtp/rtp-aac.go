package rtp

import (
    "encoding/binary"
    "errors"
    "fmt"

    "github.com/yapingcat/gomedia/go-codec"
)

//RFC3640
// mpeg4-generic
// +---------+-----------+-----------+---------------+
// | RTP     | AU Header | Auxiliary | Access Unit   |
// | Header  | Section   | Section   | Data Section  |
// +---------+-----------+-----------+---------------+
// 	<----------RTP Packet Payload----------->
//
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+- .. -+-+-+-+-+-+-+-+-+-+
// |AU-headers-length|AU-header|AU-header|      |AU-header|padding|
// |                 |   (1)   |   (2)   |      |   (n)   | bits  |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+- .. -+-+-+-+-+-+-+-+-+-+

// Au-headers-length 2 bytes

//      Au-header
// +---------------------------------------+
// |     AU-size                           |
// +---------------------------------------+
// |     AU-Index / AU-Index-delta         |
// +---------------------------------------+
// |     CTS-flag                          |
// +---------------------------------------+
// |     CTS-delta                         |
// +---------------------------------------+
// |     DTS-flag                          |
// +---------------------------------------+
// |     DTS-delta                         |
// +---------------------------------------+
// |     RAP-flag                          |
// +---------------------------------------+
// |     Stream-state                      |
// +---------------------------------------+

type AACPacker struct {
    CommPacker
    pt       uint8
    ssrc     uint32
    sequence uint16
}

func NewAACPacker(pt uint8, ssrc uint32, sequence uint16, mtu int) *AACPacker {
    return &AACPacker{
        pt:         pt,
        ssrc:       ssrc,
        sequence:   sequence,
        CommPacker: CommPacker{mtu: mtu},
    }
}

func (packer *AACPacker) Pack(data []byte, timestamp uint32) error {
    if len(data)+4+RTP_FIX_HEAD_LEN > packer.mtu {
        return errors.New("unsupport fragment aac into multi rtp packet")
    }
    fmt.Println("pack aac")
    pkg := RtpPacket{}
    pkg.Header.PayloadType = packer.pt
    pkg.Header.SequenceNumber = packer.sequence
    pkg.Header.SSRC = packer.ssrc
    pkg.Header.Timestamp = timestamp
    pkg.Header.Marker = 1
    pkg.Payload = make([]byte, 4+len(data))
    binary.BigEndian.PutUint16(pkg.Payload, 16)
    size := uint16(len(data))
    pkg.Payload[2] = uint8(size >> 5)
    pkg.Payload[3] = uint8((size & 0x1F) << 3)
    copy(pkg.Payload[4:], data)
    packer.sequence++
    if packer.onRtp != nil {
        packer.onRtp(&pkg)
    }
    if packer.onPacket != nil {
        return packer.onPacket(pkg.Encode())
    }

    return nil
}

type AACUnPacker struct {
    CommUnPacker
    sizeLength       int
    indexLength      int
    indexDeltaLength int
    duration         uint32
    constantDuration uint32
    asc              []byte
}

func NewAACUnPacker(sizeLength int, indexLength int, asc []byte) *AACUnPacker {
    unpacker := &AACUnPacker{
        sizeLength:       sizeLength,
        indexLength:      indexLength,
        indexDeltaLength: indexLength,
        duration:         1024,
        asc:              append([]byte(nil), asc...),
    }
    var config codec.AudioSpecificConfiguration
    if config.Decode(asc) == nil && config.Audio_object_type >= 1 && config.Audio_object_type <= 4 && config.GA_framelength_flag != 0 {
        unpacker.duration = 960
    }
    return unpacker
}

// SetAccessUnitTiming applies the negotiated AU index width and duration in RTP ticks.
// A zero duration uses the duration inferred from AudioSpecificConfig.
func (unpacker *AACUnPacker) SetAccessUnitTiming(indexDeltaLength int, duration uint32) {
    unpacker.indexDeltaLength = indexDeltaLength
    unpacker.constantDuration = duration
}

func (unpacker *AACUnPacker) UnPack(pkt []byte) error {
    pkg := &RtpPacket{}
    if err := pkg.Decode(pkt); err != nil {
        return err
    }
    if len(pkg.Payload) < 2 {
        return errors.New("missing AAC AU-headers-length")
    }

    if unpacker.onRtp != nil {
        unpacker.onRtp(pkg)
    }

    if unpacker.sizeLength <= 0 || unpacker.sizeLength > 32 || unpacker.indexLength < 0 || unpacker.indexLength > 32 ||
        unpacker.indexDeltaLength < 0 || unpacker.indexDeltaLength > 32 {
        return errors.New("unsupported AAC AU-header field widths")
    }
    headerBits := int(binary.BigEndian.Uint16(pkg.Payload))
    headerBytes := (headerBits + 7) / 8
    firstBits := unpacker.sizeLength + unpacker.indexLength
    nextBits := unpacker.sizeLength + unpacker.indexDeltaLength
    if headerBytes > len(pkg.Payload)-2 || headerBits < firstBits || (headerBits-firstBits)%nextBits != 0 {
        return errors.New("invalid AAC AU headers")
    }
    headers := codec.NewBitStream(pkg.Payload[2 : 2+headerBytes])
    payload := pkg.Payload[2+headerBytes:]
    count := 1 + (headerBits-firstBits)/nextBits
    timestamp := pkg.Header.Timestamp
    duration := unpacker.constantDuration
    if duration == 0 {
        duration = unpacker.duration
    }
    for i := 0; i < count; i++ {
        size := headers.GetBits(unpacker.sizeLength)
        if i == 0 {
            headers.SkipBits(unpacker.indexLength)
        } else {
            step := uint32(1)
            if unpacker.indexDeltaLength > 0 {
                step += uint32(headers.GetBits(unpacker.indexDeltaLength))
            }
            timestamp += step * duration
        }
        if size == 0 || size > uint64(len(payload)) {
            return errors.New("incomplete AAC access unit")
        }
        var adts []byte
        if len(unpacker.asc) > 0 {
            if size > 8191-7 {
                return errors.New("AAC access unit too large for ADTS")
            }
            adtsHdr, err := codec.ConvertASCToADTS(unpacker.asc, int(size)+7)
            if err != nil {
                return err
            }
            adts = adtsHdr.Encode()
        }
        if unpacker.onFrame != nil {
            unpacker.onFrame(append(adts, payload[:size]...), timestamp, false)
        }
        payload = payload[size:]
    }
    return nil
}
