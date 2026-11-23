package ogg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strconv"

	"github.com/yapingcat/gomedia/go-codec"
)

type oggCodec interface {
    codecid() codec.CodecID
    magic() []byte
    magicSize() int
}

type OpusCodec struct {
}

func (opus OpusCodec) codecid() codec.CodecID {
    return codec.CODECID_AUDIO_OPUS
}

func (opus OpusCodec) magic() []byte {
    return []byte("OpusHead")
}

func (opus OpusCodec) magicSize() int {
    return 8
}

type VP8Codec struct {
}

func (vp8 VP8Codec) codecid() codec.CodecID {
    return codec.CODECID_VIDEO_VP8
}

func (vp8 VP8Codec) magic() []byte {
    return []byte("OVP80")
}

func (vp8 VP8Codec) magicSize() int {
    return 5
}

var codecs []oggCodec

func init() {
    codecs = make([]oggCodec, 2)
    codecs[0] = OpusCodec{}
    codecs[1] = VP8Codec{}
}

type oggParser interface {
    header(stream *oggStream, packet []byte)(err error)
    packet(stream *oggStream, packet []byte) (frame []byte, pts uint64, dts uint64)
    gptopts(granulePos uint64) uint64
    extraData() []byte
}

func createParser(cid codec.CodecID) oggParser {
    switch cid {
    case codec.CODECID_AUDIO_OPUS:
        return &opusDemuxer{}
    case codec.CODECID_VIDEO_VP8:
        return &vp8Demuxer{
            lastpts: ^uint64(0),
            pktIdx:  0,
        }
    default:
        panic("unsupport codecid")
    }
}

type opusDemuxer struct {
    extradata   []byte
    ctx         codec.OpusContext
    nextGranule uint64
    pageSeq     uint32
    hasAudio    bool
}

// opus ID head
// 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |      'O'      |      'p'      |      'u'      |      's'      |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |      'H'      |      'e'      |      'a'      |      'd'      |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |  Version = 1  | Channel Count |           Pre-skip            |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                     Input Sample Rate (Hz)                    |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |   Output Gain (Q7.8 in dB)    | Mapping Family|               |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+               :
// |                                                               |
// :               Optional Channel Mapping Table...               :
// |                                                               |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

func (opus *opusDemuxer) header(stream *oggStream, packet []byte) (err error) {
    if bytes.HasPrefix(packet, []byte("OpusHead")) {
        if err := opus.ctx.ParseExtranData(packet); err != nil {
            return err
        }
        opus.extradata = append([]byte(nil), packet...)
        return nil
    }
    if bytes.HasPrefix(packet, []byte("OpusTags")) {
        return nil
    }
    return errors.New(`unsupported opus header` + strconv.Quote(string(packet)))
}

func (opus *opusDemuxer) packet(stream *oggStream, packet []byte) ([]byte, uint64, uint64) {
    page := stream.currentPage
    if stream.lost == 0 && (!opus.hasAudio || opus.pageSeq != page.pageSeq) {
        // After earlier audio, the EOS granule trims the end of the page.
        // Carry forward the previous timestamp only across consecutive audio.
        if !page.eos || !opus.hasAudio || stream.discontinuity {
            var duration uint64
            for _, pkt := range page.packets {
                duration += codec.OpusPacketDuration(pkt)
            }
            opus.nextGranule = 0
            if page.granulePos >= duration {
                opus.nextGranule = page.granulePos - duration
            }
        }
        opus.pageSeq = page.pageSeq
        opus.hasAudio = true
    }

    // The callback uses unsigned timestamps. Clamp packets in pre-skip to zero.
    var pts uint64
    if preskip := uint64(opus.ctx.Preskip); opus.nextGranule >= preskip {
        pts = opus.nextGranule - preskip
    }
    if stream.lost == 0 {
        opus.nextGranule += codec.OpusPacketDuration(packet)
    }
    return packet, pts, pts
}
func (opus *opusDemuxer) gptopts(granulePos uint64) uint64 {
    return 0
}

func (opus *opusDemuxer) extraData() []byte {
    return opus.extradata
}

//ffmpeg oggparsevp8.c
type vp8Demuxer struct {
    pktIdx            uint64
    lastpts           uint64
    granule           uint64
    width             uint16
    height            uint16
    sampleAspectratio uint32
    frameRate         uint32
    extradata         []byte
}

func (vp8 *vp8Demuxer) header(stream *oggStream, packet []byte) (err error) {
    if !bytes.Equal([]byte("OVP80"), packet[0:5]) {
        return
    }

    switch packet[5] {
    case 0x01:
        if packet[6] != 1 {
            return
        }
        vp8.width = binary.BigEndian.Uint16(packet[8:])
        vp8.height = binary.BigEndian.Uint16(packet[10:])
        num := uint32(packet[12])
        num = (num << 8) | uint32(packet[13])
        num = (num << 8) | uint32(packet[14])
        den := uint32(packet[15])
        den = (den << 8) | uint32(packet[16])
        den = (den << 8) | uint32(packet[17])
        vp8.sampleAspectratio = num / den
        num = binary.BigEndian.Uint32(packet[18:])
        den = binary.BigEndian.Uint32(packet[22:])
        vp8.frameRate = num / den
        vp8.extradata = make([]byte, len(packet))
        copy(vp8.extradata, packet)
    case 0x02:
        if packet[6] != 0x20 {
            return
        }
        //TODO Parse Comment
    default:
        return nil
    }
    return nil
}

func (vp8 *vp8Demuxer) packet(stream *oggStream, packet []byte) (frame []byte, pts uint64, dts uint64) {

    if stream.lost == 1 {
        return packet, vp8.lastpts, vp8.lastpts
    }

    if vp8.granule != stream.currentPage.granulePos {
        vp8.lastpts = 0
        vp8.pktIdx = 0
        vp8.granule = stream.currentPage.granulePos
    }
    var duration uint64 = 0
    for i := int(vp8.pktIdx); i < len(stream.currentPage.packets); i++ {
        candidate := stream.currentPage.packets[i]
        if len(candidate) > 0 {
            duration += uint64((candidate[0] >> 4) & 1)
        }
    }
    vp8.lastpts = vp8.gptopts(stream.currentPage.granulePos) - duration
    frame = packet
    pts = vp8.lastpts
    dts = pts
    vp8.pktIdx++
    return
}

func (vp8 *vp8Demuxer) gptopts(granulePos uint64) uint64 {
    var invcnt uint64 = 0
    if ((granulePos >> 30) & 3) == 0 {
        invcnt = 1
    }
    pts := (granulePos >> 32) - invcnt
    return pts
}

func (vp8 *vp8Demuxer) extraData() []byte {
    return vp8.extradata
}
