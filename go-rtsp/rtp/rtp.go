package rtp

import (
    "bytes"
    "encoding/binary"
    "errors"

    "github.com/yapingcat/gomedia/go-codec"
)

type RTP_HOOK_FUNC func(pkg *RtpPacket)
type ON_RTP_PKT_FUNC func(pkt []byte) error
type ON_FRAME_FUNC func(frame []byte, timestamp uint32, lost bool)

type Packer interface {
    Pack(data []byte, timestamp uint32) error
    HookRtp(cb RTP_HOOK_FUNC)
    SetMtu(mtu int)
    OnPacket(onPkt ON_RTP_PKT_FUNC)
}

type CommPacker struct {
    onPacket ON_RTP_PKT_FUNC
    onRtp    RTP_HOOK_FUNC
    mtu      int
}

func (pack *CommPacker) OnPacket(onPkt ON_RTP_PKT_FUNC) {
    pack.onPacket = onPkt
}

func (pack *CommPacker) SetMtu(mtu int) {
    pack.mtu = mtu
}

func (pack *CommPacker) HookRtp(cb RTP_HOOK_FUNC) {
    pack.onRtp = cb
}

func splitRTPAccessUnit(frame []byte, nalus [][]byte) [][]byte {
    codec.SplitFrame(frame, func(nalu []byte) bool {
        if len(nalu) > 0 {
            nalus = append(nalus, nalu)
        }
        return true
    })
    return nalus
}

type UnPacker interface {
    OnFrame(onframe ON_FRAME_FUNC)
    UnPack(pkt []byte) error
    HookRtp(cb RTP_HOOK_FUNC)
}

type CommUnPacker struct {
    onFrame ON_FRAME_FUNC
    onRtp   RTP_HOOK_FUNC
}

func (unpack *CommUnPacker) OnFrame(onframe ON_FRAME_FUNC) {
    unpack.onFrame = onframe
}

func (unpack *CommUnPacker) HookRtp(cb RTP_HOOK_FUNC) {
    unpack.onRtp = cb
}

// An Annex B assembly buffer retains its start code between NAL units.
func (unpack *CommUnPacker) finishIncompleteNAL(buffer *bytes.Buffer, timestamp uint32) {
    if buffer.Len() <= 4 {
        return
    }
    if unpack.onFrame != nil {
        unpack.onFrame(buffer.Bytes(), timestamp, true)
    }
    buffer.Truncate(4)
}

type RtpPacket struct {
    Header     RtpHdr
    Extensions []byte
    Payload    []byte
    Padding    []byte
}

func (pkg *RtpPacket) Decode(data []byte) error {
    offset, err := pkg.Header.Decode(data)
    if err != nil {
        return err
    }

    data = data[offset:]
    if pkg.Header.ExtensionFlag > 0 {
        if len(data) < 4 {
            return errors.New("rtp extension need 4 bytes at least")
        }
        length := binary.BigEndian.Uint16(data[2:])
        if len(data)-4 < int(length)*4 {
            return errors.New("rtp extension need more bytes")
        }
        pkg.Extensions = data[:4+4*length]
        data = data[4+4*length:]
    }
    if pkg.Header.PaddingFlag > 0 {
        if len(data) == 0 || int(data[len(data)-1]) > len(data) {
            return errors.New("rtp padding need more bytes")
        }
        pkg.Padding = data[len(data)-int(data[len(data)-1]):]
        data = data[:len(data)-int(data[len(data)-1])]
    }
    pkg.Payload = data
    return nil
}

func (pkg *RtpPacket) Encode() []byte {
    if len(pkg.Extensions) > 0 {
        pkg.Header.ExtensionFlag = 1
    }
    if len(pkg.Padding) > 0 {
        pkg.Header.PaddingFlag = 1
    }

    hdr := pkg.Header.Encode()

    data := make([]byte, 0, len(hdr)+len(pkg.Extensions)+len(pkg.Payload)+len(pkg.Padding))
    data = append(data, hdr...)
    data = append(data, pkg.Extensions...)
    data = append(data, pkg.Payload...)
    data = append(data, pkg.Padding...)
    return data
}
