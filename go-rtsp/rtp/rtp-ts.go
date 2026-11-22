package rtp

import (
    "bytes"
    "errors"
)

const mpegTSPacketSize = 188

type TsPacker struct {
    CommPacker
    pt       uint8
    ssrc     uint32
    sequence uint16
}

func NewTsPacker(pt uint8, ssrc uint32, sequence uint16, mtu int) *TsPacker {
    return &TsPacker{
        pt:         pt,
        ssrc:       ssrc,
        sequence:   sequence,
        CommPacker: CommPacker{mtu: mtu},
    }
}

func (pack *TsPacker) Pack(data []byte, timestamp uint32) error {
    if len(data) == 0 {
        return nil
    }
    if len(data)%mpegTSPacketSize != 0 {
        return errors.New("RTP MPEG-TS input must contain whole transport packets")
    }
    if pack.mtu < RTP_FIX_HEAD_LEN+mpegTSPacketSize {
        return errors.New("MTU too small for an RTP MPEG-TS packet")
    }
    maxPayload := (pack.mtu - RTP_FIX_HEAD_LEN) / mpegTSPacketSize * mpegTSPacketSize
    for len(data) > 0 {
        payloadLength := len(data)
        if payloadLength > maxPayload {
            payloadLength = maxPayload
        }
        pkg := RtpPacket{}
        pkg.Header.PayloadType = pack.pt
        pkg.Header.SequenceNumber = pack.sequence
        pkg.Header.SSRC = pack.ssrc
        pkg.Header.Timestamp = timestamp
        pkg.Payload = append([]byte(nil), data[:payloadLength]...)
        data = data[payloadLength:]
        if pack.onRtp != nil {
            pack.onRtp(&pkg)
        }
        pack.sequence++
        if pack.onPacket != nil {
            if err := pack.onPacket(pkg.Encode()); err != nil {
                return err
            }
        }
    }
    return nil
}

type TsUnPacker struct {
    CommUnPacker
    timestamp    int64
    lastSequence uint16
    lost         bool
    frameBuffer  *bytes.Buffer
}

func NewTsUnPacker() *TsUnPacker {
    return &TsUnPacker{
        frameBuffer: new(bytes.Buffer),
        timestamp:   -1,
    }
}

func (unpacker *TsUnPacker) UnPack(pkt []byte) error {
    pkg := &RtpPacket{}
    if err := pkg.Decode(pkt); err != nil {
        return err
    }

    if unpacker.onRtp != nil {
        unpacker.onRtp(pkg)
    }

    //first rtp packet
    if unpacker.timestamp == -1 {
        unpacker.timestamp = int64(pkg.Header.Timestamp)
        unpacker.lastSequence = pkg.Header.SequenceNumber
        unpacker.lost = false
        unpacker.frameBuffer.Write(pkg.Payload)
        return nil
    }

    if unpacker.lastSequence+1 != pkg.Header.SequenceNumber {
        unpacker.lost = true
    }

    //时间戳作为帧分割标志
    if unpacker.timestamp != int64(pkg.Header.Timestamp) {
        if unpacker.onFrame != nil {
            unpacker.onFrame(unpacker.frameBuffer.Bytes(), uint32(unpacker.timestamp), unpacker.lost)
        }

        if unpacker.lastSequence+1 == pkg.Header.SequenceNumber {
            unpacker.lost = false
        }
        unpacker.frameBuffer.Reset()
    }
    unpacker.timestamp = int64(pkg.Header.Timestamp)
    unpacker.lastSequence = pkg.Header.SequenceNumber
    unpacker.frameBuffer.Write(pkg.Payload)
    return nil
}
