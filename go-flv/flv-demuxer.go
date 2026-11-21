package flv

import (
    "bytes"
    "encoding/binary"
    "errors"

    "github.com/yapingcat/gomedia/go-codec"
)

type OnVideoFrameCallBack func(codecid codec.CodecID, frame []byte, cts int)
type VideoTagDemuxer interface {
    Decode(data []byte) error
    OnFrame(onframe OnVideoFrameCallBack)
}

type AVCTagDemuxer struct {
    spss    map[uint64][]byte
    ppss    map[uint64][]byte
    onframe OnVideoFrameCallBack
}

func NewAVCTagDemuxer() *AVCTagDemuxer {
    return &AVCTagDemuxer{
        spss:    make(map[uint64][]byte),
        ppss:    make(map[uint64][]byte),
        onframe: nil,
    }
}

func (demuxer *AVCTagDemuxer) OnFrame(onframe OnVideoFrameCallBack) {
    demuxer.onframe = onframe
}

func (demuxer *AVCTagDemuxer) cacheParameterSet(nalu []byte) {
    if len(nalu) < 6 {
        return
    }
    sets, maxID := &demuxer.spss, uint64(31)
    bs := codec.NewBitStream(codec.CovertRbspToSodb(nalu[5:]))
    switch codec.H264NaluTypeWithoutStartCode(nalu[4:]) {
    case codec.H264_NAL_SPS:
        bs.SkipBits(24) // profile, constraints and level
    case codec.H264_NAL_PPS:
        sets, maxID = &demuxer.ppss, 255
    default:
        return
    }
    id, ok := readParameterSetID(bs, maxID)
    if !ok {
        return
    }
    if *sets == nil {
        *sets = make(map[uint64][]byte)
    }
    if !bytes.Equal((*sets)[id], nalu) {
        (*sets)[id] = append([]byte(nil), nalu...)
    }
}

func (demuxer *AVCTagDemuxer) Decode(data []byte) error {

    if len(data) < 5 {
        return errors.New("avc tag size < 5")
    }

    vtag := VideoTag{}
    vtag.Decode(data[0:5])
    data = data[5:]
    if vtag.AVCPacketType == AVC_SEQUENCE_HEADER {
        tmpspss, tmpppss := codec.CovertExtradata(data)
        for _, sps := range tmpspss {
            demuxer.cacheParameterSet(sps)
        }
        for _, pps := range tmpppss {
            demuxer.cacheParameterSet(pps)
        }
    } else {
        var hassps bool
        var haspps bool
        var idr bool
        var parameterSets [][]byte
        tmpdata := data
        for len(tmpdata) > 0 {
            naluSize := binary.BigEndian.Uint32(tmpdata)
            codec.CovertAVCCToAnnexB(tmpdata)
            naluType := codec.H264NaluType(tmpdata)
            if naluType == codec.H264_NAL_I_SLICE {
                idr = true
            } else if naluType == codec.H264_NAL_SPS {
                hassps = true
                parameterSets = append(parameterSets, tmpdata[:4+naluSize])
            } else if naluType == codec.H264_NAL_PPS {
                haspps = true
                parameterSets = append(parameterSets, tmpdata[:4+naluSize])
            } else if naluType < codec.H264_NAL_I_SLICE {
                sh := codec.SliceHeader{}
                sh.Decode(codec.NewBitStream(tmpdata[5:]))
                if sh.Slice_type == 2 || sh.Slice_type == 7 {
                    idr = true
                }
            }
            tmpdata = tmpdata[4+naluSize:]
        }
        for _, nalu := range parameterSets {
            demuxer.cacheParameterSet(nalu)
        }

        if idr && (!hassps || !haspps) {
            var nalus []byte = make([]byte, 0, 2048)
            for _, sps := range demuxer.spss {
                nalus = append(nalus, sps...)
            }
            for _, pps := range demuxer.ppss {
                nalus = append(nalus, pps...)
            }
            nalus = append(nalus, data...)
            if demuxer.onframe != nil {
                demuxer.onframe(codec.CODECID_VIDEO_H264, nalus, int(vtag.CompositionTime))
            }
        } else {
            if demuxer.onframe != nil && len(data) > 0 {
                demuxer.onframe(codec.CODECID_VIDEO_H264, data, int(vtag.CompositionTime))
            }
        }
    }
    return nil
}

type HevcTagDemuxer struct {
    SpsPpsVps []byte
    onframe   OnVideoFrameCallBack
    naluLengthSize int
}

func NewHevcTagDemuxer() *HevcTagDemuxer {
    return &HevcTagDemuxer{
        SpsPpsVps: make([]byte, 0),
        onframe:   nil,
    }
}

func (demuxer *HevcTagDemuxer) OnFrame(onframe OnVideoFrameCallBack) {
    demuxer.onframe = onframe
}

func (demuxer *HevcTagDemuxer) Decode(data []byte) error {
    return demuxer.decodeHEVCTag(data)
}

type OnAudioFrameCallBack func(codecid codec.CodecID, frame []byte)

type AudioTagDemuxer interface {
    Decode(data []byte) error
    OnFrame(onframe OnAudioFrameCallBack)
}

type AACTagDemuxer struct {
    asc     []byte
    onframe OnAudioFrameCallBack
}

func NewAACTagDemuxer() *AACTagDemuxer {
    return &AACTagDemuxer{
        asc:     make([]byte, 0, 2),
        onframe: nil,
    }
}

func (demuxer *AACTagDemuxer) OnFrame(onframe OnAudioFrameCallBack) {
    demuxer.onframe = onframe
}

func (demuxer *AACTagDemuxer) Decode(data []byte) error {

    if len(data) < 2 {
        return errors.New("aac tag size < 2")
    }

    atag := AudioTag{}
    err := atag.Decode(data[0:2])
    if err != nil {
        return err
    }
    if atag.SoundFormat != uint8(FLV_AAC) {
        return errors.New("unexpected AAC sound format")
    }
    data = data[2:]
    switch atag.AACPacketType {
    case AAC_SEQUENCE_HEADER:
        if len(data) < 2 {
            return errors.New("truncated AAC decoder configuration")
        }
        demuxer.asc = make([]byte, len(data))
        copy(demuxer.asc, data)
    case AAC_RAW:
        const adtsHeaderSize = 7
        const maxADTSFrameLength = (1 << 13) - 1
        if len(data) == 0 || len(data) > maxADTSFrameLength-adtsHeaderSize {
            return errors.New("invalid AAC payload length for ADTS")
        }
        adts, err := codec.ConvertASCToADTS(demuxer.asc, len(data)+adtsHeaderSize)
        if err != nil {
            return err
        }
        adts_frame := append(adts.Encode(), data...)
        if demuxer.onframe != nil {
            demuxer.onframe(codec.CODECID_AUDIO_AAC, adts_frame)
        }
    default:
        return errors.New("unsupported AAC packet type")
    }
    return nil
}

type G711Demuxer struct {
    format  FLV_SOUND_FORMAT
    onframe OnAudioFrameCallBack
}

func NewG711Demuxer(format FLV_SOUND_FORMAT) *G711Demuxer {
    return &G711Demuxer{
        format:  format,
        onframe: nil,
    }
}

func (demuxer *G711Demuxer) OnFrame(onframe OnAudioFrameCallBack) {
    demuxer.onframe = onframe
}

func (demuxer *G711Demuxer) Decode(data []byte) error {

    if len(data) < 1 {
        return errors.New("audio tag size < 1")
    }

    atag := AudioTag{}
    err := atag.Decode(data[0:1])
    if err != nil {
        return err
    }
    data = data[1:]

    if demuxer.onframe != nil {
        demuxer.onframe(demuxer.format.ToMpegCodecId(), data)
    }
    return nil
}

func CreateAudioTagDemuxer(formats FLV_SOUND_FORMAT) (demuxer AudioTagDemuxer) {
    switch formats {
    case FLV_G711A, FLV_G711U, FLV_MP3:
        demuxer = NewG711Demuxer(formats)
    case FLV_AAC:
        demuxer = NewAACTagDemuxer()
    default:
        panic("unsupport audio codec id")
    }
    return
}

func CreateFlvVideoTagHandle(cid FLV_VIDEO_CODEC_ID) (demuxer VideoTagDemuxer) {
    switch cid {
    case FLV_AVC:
        demuxer = NewAVCTagDemuxer()
    case FLV_HEVC:
        demuxer = NewHevcTagDemuxer()
    default:
        panic("unsupport audio codec id")
    }
    return
}
