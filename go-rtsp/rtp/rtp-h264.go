package rtp

import (
    "bytes"
    "encoding/binary"
    "errors"

    "github.com/yapingcat/gomedia/go-codec"
)

// rfc6184 https://datatracker.ietf.org/doc/html/rfc6184
//
// Payload Packet    Single NAL    Non-Interleaved    Interleaved
// Type    Type      Unit Mode           Mode             Mode
// -------------------------------------------------------------
// 0      reserved      ig               ig               ig
// 1-23   NAL unit     yes              yes               no
// 24     STAP-A        no              yes               no
// 25     STAP-B        no               no              yes
// 26     MTAP16        no               no              yes
// 27     MTAP24        no               no              yes
// 28     FU-A          no              yes              yes
// 29     FU-B          no               no              yes
// 30-31  reserved      ig               ig               ig
//

type H264Packer struct {
    CommPacker
    ssrc     uint32
    pt       uint8
    sequence uint16
    stap_a   bool
}

func NewH264Packer(pt uint8, ssrc uint32, sequence uint16, mtu int) *H264Packer {
    return &H264Packer{
        pt:         pt,
        ssrc:       ssrc,
        sequence:   sequence,
        stap_a:     false,
        CommPacker: CommPacker{mtu: mtu},
    }
}

func (pack *H264Packer) EnableStapA() {
    pack.stap_a = true
}

func (pack *H264Packer) Pack(frame []byte, timestamp uint32) error {
    var scratch [8][]byte
    nalus := splitRTPAccessUnit(frame, scratch[:0])
    for i := 0; i < len(nalus); i++ {
        nalu := nalus[i]
        if pack.stap_a && i+1 < len(nalus) && pack.canAggregateParameterSets(nalu, nalus[i+1]) {
            if err := pack.packStapA(nalus[i:i+2], timestamp, i+2 == len(nalus)); err != nil {
                return err
            }
            i++
            continue
        }
        var err error
        last := i+1 == len(nalus)
        if len(nalu)+RTP_FIX_HEAD_LEN <= pack.mtu {
            err = pack.packSingleNalu(nalu, timestamp, last)
        } else if pack.mtu <= RTP_FIX_HEAD_LEN+2 {
            return errors.New("MTU too small for an H.264 RTP fragment")
        } else {
            err = pack.packFuA(nalu, timestamp, last)
        }
        if err != nil {
            return err
        }
    }
    return nil
}

func (pack *H264Packer) canAggregateParameterSets(first, second []byte) bool {
    const stapHeaderSize = 5 // One STAP header and two uint16 lengths.
    if len(first) > 65535 || len(second) > 65535 || RTP_FIX_HEAD_LEN+stapHeaderSize+len(first)+len(second) > pack.mtu {
        return false
    }
    firstType := codec.H264NaluTypeWithoutStartCode(first)
    secondType := codec.H264NaluTypeWithoutStartCode(second)
    return (firstType == codec.H264_NAL_SPS && secondType == codec.H264_NAL_PPS) ||
        (firstType == codec.H264_NAL_PPS && secondType == codec.H264_NAL_SPS)
}

func (pack *H264Packer) packSingleNalu(nalu []byte, timestamp uint32, last bool) error {
    pkg := RtpPacket{}
    pkg.Header.PayloadType = pack.pt
    pkg.Header.SSRC = pack.ssrc
    pkg.Header.SequenceNumber = pack.sequence
    pkg.Header.Timestamp = timestamp
    if last {
        pkg.Header.Marker = 1
    }
    pkg.Payload = nalu
    pack.sequence++
    if pack.onRtp != nil {
        pack.onRtp(&pkg)
    }
    if pack.onPacket != nil {
        return pack.onPacket(pkg.Encode())
    }
    return nil
}

//  0                   1                   2                   3
//  0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// | FU indicator  |   FU header   |                               |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+                               |
// |                                                               |
// |                         FU payload                            |
// |                                                               |
// |                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                               :...OPTIONAL RTP padding        |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

// FU indicator
// +---------------+
// |0|1|2|3|4|5|6|7|
// +-+-+-+-+-+-+-+-+
// |F|NRI|  Type   |
// +---------------+

// FU header
// +---------------+
// |0|1|2|3|4|5|6|7|
// +-+-+-+-+-+-+-+-+
// |S|E|R|  Type   |
// +---------------+

func (pack *H264Packer) packFuA(nalu []byte, timestamp uint32, last bool) (err error) {
    var fuIndicator byte = nalu[0]&0xE0 | 0x1c
    var fuHeader byte = nalu[0]&0x1F | 0x80
    nalu = nalu[1:]
    for {
        pkg := RtpPacket{}
        pkg.Header.PayloadType = pack.pt
        pkg.Header.SSRC = pack.ssrc
        pkg.Header.SequenceNumber = pack.sequence
        pkg.Header.Timestamp = timestamp
        if len(nalu)+RTP_FIX_HEAD_LEN+2 <= pack.mtu {
            if last {
                pkg.Header.Marker = 1
            }
            fuHeader |= 0x40
            pkg.Payload = make([]byte, 0, 2+len(nalu))
            pkg.Payload = append(pkg.Payload, fuIndicator)
            pkg.Payload = append(pkg.Payload, fuHeader)
            pkg.Payload = append(pkg.Payload, nalu...)
            if pack.onRtp != nil {
                pack.onRtp(&pkg)
            }
            if pack.onPacket != nil {
                err = pack.onPacket(pkg.Encode())
            }
            pack.sequence++
            return
        }
        pkg.Payload = make([]byte, 0, 2+pack.mtu)
        pkg.Payload = append(pkg.Payload, fuIndicator)
        pkg.Payload = append(pkg.Payload, fuHeader)
        if fuHeader&0x80 > 0 {
            fuHeader &= 0x7F
        }
        pkg.Payload = append(pkg.Payload, nalu[:pack.mtu-2-RTP_FIX_HEAD_LEN]...)
        nalu = nalu[pack.mtu-2-RTP_FIX_HEAD_LEN:]
        if pack.onRtp != nil {
            pack.onRtp(&pkg)
        }
        if pack.onPacket != nil {
            err = pack.onPacket(pkg.Encode())
        }
        pack.sequence++
        if err != nil {
            return
        }
    }
}

//  0                   1                   2                   3
//  0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                          RTP Header                           |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |STAP-A NAL HDR |         NALU 1 Size           | NALU 1 HDR    |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                         NALU 1 Data                           |
// :                                                               :
// +               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |               | NALU 2 Size                   | NALU 2 HDR    |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                         NALU 2 Data                           |
// :                                                               :
// |                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                               :...OPTIONAL RTP padding        |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

func (pack *H264Packer) packStapA(nalus [][]byte, timestamp uint32, last bool) error {
    pkg := RtpPacket{}
    pkg.Header.PayloadType = pack.pt
    pkg.Header.SSRC = pack.ssrc
    pkg.Header.SequenceNumber = pack.sequence
    pkg.Header.Timestamp = timestamp
    if last {
        pkg.Header.Marker = 1
    }

    length := 0
    var forbidden, nri byte
    for _, nalu := range nalus {
        length += len(nalu) + 2
        forbidden |= nalu[0] & 0x80
        if value := nalu[0] & 0x60; value > nri {
            nri = value
        }
    }

    pkg.Payload = make([]byte, 1, length+1)
    pkg.Payload[0] = forbidden | nri | 24
    for _, nalu := range nalus {
        var tmp [2]byte
        binary.BigEndian.PutUint16(tmp[:], uint16(len(nalu)))
        pkg.Payload = append(pkg.Payload, tmp[:]...)
        pkg.Payload = append(pkg.Payload, nalu...)
    }
    if pack.onRtp != nil {
        pack.onRtp(&pkg)
    }
    pack.sequence++
    if pack.onPacket != nil {
        return pack.onPacket(pkg.Encode())
    }
    return nil
}

type H264UnPacker struct {
    CommUnPacker
    timestamp    uint32
    lastSequence uint16
    lost         bool
    frameBuffer  *bytes.Buffer
}

func NewH264UnPacker() *H264UnPacker {
    unpacker := &H264UnPacker{
        frameBuffer: new(bytes.Buffer),
    }
    unpacker.frameBuffer.Grow(1500)
    unpacker.frameBuffer.Write([]byte{0x00, 0x00, 0x00, 0x01})
    return unpacker
}

func (unpacker *H264UnPacker) UnPack(pkt []byte) error {
    pkg := &RtpPacket{}
    if err := pkg.Decode(pkt); err != nil {
        return err
    }
    
    if len(pkg.Payload) == 0 {
        return nil
    }

    if unpacker.onRtp != nil {
        unpacker.onRtp(pkg)
    }

    packType := pkg.Payload[0] & 0x1f
    switch {
    case 0 < packType && packType < 24:
        unpacker.finishIncompleteNAL(unpacker.frameBuffer, unpacker.timestamp)
        unpacker.frameBuffer.Write(pkg.Payload)
        if unpacker.onFrame != nil {
            unpacker.onFrame(unpacker.frameBuffer.Bytes(), pkg.Header.Timestamp, false)
        }
        unpacker.frameBuffer.Truncate(4)
    case packType == 24:
        unpacker.finishIncompleteNAL(unpacker.frameBuffer, unpacker.timestamp)
        return unpacker.unpackStapa(pkg)
    case packType == 25:
        fallthrough
    case packType == 26:
        fallthrough
    case packType == 27:
        return errors.New("unsupport h264 rtp packet type")
    case packType == 28:
        return unpacker.unpackFuA(pkg)
    case packType == 29:
        fallthrough
    default:
        return errors.New("unsupport h264 rtp packet type")
    }
    return nil
}

func (unpacker *H264UnPacker) unpackFuA(pkt *RtpPacket) error {
    s := pkt.Payload[1] & 0x80
    e := pkt.Payload[1] & 0x40
    if s > 0 {
        unpacker.finishIncompleteNAL(unpacker.frameBuffer, unpacker.timestamp)
        unpacker.timestamp = pkt.Header.Timestamp
        unpacker.frameBuffer.WriteByte((pkt.Payload[0] & 0xE0) | (pkt.Payload[1] & 0x1F))
        unpacker.lost = false
    } else {
        if unpacker.lastSequence+1 != pkt.Header.SequenceNumber {
            unpacker.lost = true
        }
    }
    unpacker.lastSequence = pkt.Header.SequenceNumber
    unpacker.frameBuffer.Write(pkt.Payload[2:])
    if e > 0 {
        if unpacker.onFrame != nil {
            unpacker.onFrame(unpacker.frameBuffer.Bytes(), unpacker.timestamp, unpacker.lost)
        }
        unpacker.frameBuffer.Truncate(4)
    }
    return nil
}

func (unpacker *H264UnPacker) unpackStapa(pkt *RtpPacket) error {
    nalus := pkt.Payload[1:]
    for len(nalus) > 0 {
        naluLength := binary.BigEndian.Uint16(nalus)
        if len(nalus)-2 < int(naluLength) {
            return errors.New("need more bytes")
        }
        unpacker.frameBuffer.Write(nalus[2 : 2+naluLength])
        if unpacker.onFrame != nil {
            unpacker.onFrame(unpacker.frameBuffer.Bytes(), pkt.Header.Timestamp, false)
        }
        nalus = nalus[2+naluLength:]
        unpacker.frameBuffer.Truncate(4)
    }
    return nil
}
