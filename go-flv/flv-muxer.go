package flv

import (
    "bytes"
    "errors"

    "github.com/yapingcat/gomedia/go-codec"
)

func WriteAudioTag(data []byte, cid FLV_SOUND_FORMAT, sampleRate int, channelCount int, isSequenceHeader bool) []byte {
    var atag AudioTag
    atag.SoundFormat = uint8(cid)
    if cid == FLV_AAC {
        atag.SoundRate = uint8(FLV_SAMPLE_44000)
        atag.SoundSize = 1
        atag.SoundType = 1
    } else {
        switch sampleRate {
        case 5500:
            atag.SoundRate = uint8(FLV_SAMPLE_5500)
        case 11025:
            atag.SoundRate = uint8(FLV_SAMPLE_11000)
        case 22050:
            atag.SoundRate = uint8(FLV_SAMPLE_22000)
        case 44100:
            atag.SoundRate = uint8(FLV_SAMPLE_44000)
        default:
            atag.SoundRate = uint8(FLV_SAMPLE_44000)
        }
        atag.SoundSize = 1
        if channelCount > 1 {
            atag.SoundType = 1
        } else {
            atag.SoundType = 0
        }
    }

    if isSequenceHeader {
        atag.AACPacketType = 0
    } else {
        atag.AACPacketType = 1
    }
    tagData := atag.Encode()
    tagData = append(tagData, data...)
    return tagData
}

func WriteVideoTag(data []byte, isKey bool, cid FLV_VIDEO_CODEC_ID, cts int32, isSequenceHeader bool) []byte {
    var vtag VideoTag
    vtag.CodecId = uint8(cid)
    vtag.CompositionTime = cts
    if isKey {
        vtag.FrameType = uint8(KEY_FRAME)
    } else {
        vtag.FrameType = uint8(INTER_FRAME)
    }
    if isSequenceHeader {
        vtag.AVCPacketType = uint8(AVC_SEQUENCE_HEADER)
    } else {
        vtag.AVCPacketType = uint8(AVC_NALU)
    }
    tagData := vtag.Encode()
    tagData = append(tagData, data...)
    return tagData
}

type AVTagMuxer interface {
    Write(frames []byte, pts uint32, dts uint32) [][]byte
}

type AVCMuxer struct {
    spsset map[uint64][]byte
    ppsset map[uint64][]byte
    cache  []byte
}

func NewAVCMuxer() *AVCMuxer {
    return &AVCMuxer{
        spsset: make(map[uint64][]byte),
        ppsset: make(map[uint64][]byte),
        cache:  make([]byte, 0, 1024),
    }
}

func (muxer *AVCMuxer) Write(frames []byte, pts uint32, dts uint32) [][]byte {
    var vcl bool = false
    var isKey bool = false
    configurationChanged := false
    codec.SplitFrameWithStartCode(frames, func(nalu []byte) bool {
        start, sc := codec.FindStartCode(nalu, 0)
        payload := nalu[start+int(sc):]
        if len(payload) == 0 {
            return true
        }
        naltype := codec.H264NaluTypeWithoutStartCode(payload)
        switch naltype {
        case codec.H264_NAL_SPS, codec.H264_NAL_PPS:
            var sets map[uint64][]byte
            var id uint64
            if naltype == codec.H264_NAL_SPS {
                sets, id = muxer.spsset, codec.GetSPSId(payload)
            } else {
                sets, id = muxer.ppsset, codec.GetPPSId(payload)
            }
            if cached := sets[id]; cached == nil || !bytes.Equal(cached[4:], payload) {
                sets[id] = append([]byte{0, 0, 0, 1}, payload...)
                muxer.cache = appendLengthPrefixedNAL(muxer.cache, payload)
                configurationChanged = true
            }
        default:
            if naltype <= codec.H264_NAL_I_SLICE {
                vcl = true
                if naltype == codec.H264_NAL_I_SLICE {
                    isKey = true
                }
            }
            muxer.cache = appendLengthPrefixedNAL(muxer.cache, payload)
        }
        return true
    })
    var tags [][]byte
    if configurationChanged && len(muxer.ppsset) > 0 && len(muxer.spsset) > 0 {
        spss := make([][]byte, 0, len(muxer.spsset))
        for _, sps := range muxer.spsset {
            spss = append(spss, sps)
        }
        ppss := make([][]byte, 0, len(muxer.ppsset))
        for _, pps := range muxer.ppsset {
            ppss = append(ppss, pps)
        }
        extraData, _ := codec.CreateH264AVCCExtradata(spss, ppss)
        tags = append(tags, WriteVideoTag(extraData, true, FLV_AVC, 0, true))
    }

    if vcl {
        tags = append(tags, WriteVideoTag(muxer.cache, isKey, FLV_AVC, int32(pts-dts), false))
        muxer.cache = muxer.cache[:0]
    }
    return tags
}

type HevcMuxer struct {
    parameterSets []byte
    cache         []byte
    sequenceSent  bool
}

func NewHevcMuxer() *HevcMuxer {
    return &HevcMuxer{
        cache: make([]byte, 0, 1024),
    }
}

func (muxer *HevcMuxer) Write(frames []byte, pts uint32, dts uint32) [][]byte {
    var vcl bool = false
    var isKey bool = false
    var updates [][]byte
    codec.SplitFrameWithStartCode(frames, func(nalu []byte) bool {
        naltype := codec.H265NaluType(nalu)
        switch naltype {
        case codec.H265_NAL_VPS, codec.H265_NAL_SPS, codec.H265_NAL_PPS:
            start, sc := codec.FindStartCode(nalu, 0)
            updates = append(updates, nalu[start+int(sc):])
        default:
            if naltype >= 16 && naltype <= 21 {
                isKey = true
            }
            vcl = vcl || codec.IsH265VCLNaluType(naltype)
        }
        // ConvertAnnexBToAVCC rewrites four-byte start codes in place.
        // The caller may retain this frame or share it with other consumers.
        muxer.cache = appendHEVCNAL(muxer.cache, nalu)
        return true
    })
    var tags [][]byte
    var configurationChanged bool
    muxer.parameterSets, configurationChanged = mergeHEVCParameterSets(muxer.parameterSets, updates)
    if configurationChanged {
        if extraData, err := encodeHEVCConfiguration(muxer.parameterSets); err == nil {
            tags = append(tags, WriteVideoTag(extraData, true, FLV_HEVC, 0, true))
            muxer.sequenceSent = true
        }
    }
    if vcl {
        if muxer.sequenceSent {
            tags = append(tags, WriteVideoTag(muxer.cache, isKey, FLV_HEVC, int32(pts-dts), false))
        }
        // Drop an early frame if VPS/SPS/PPS are still missing. Do not merge
        // it into a later access unit with a different timestamp.
        muxer.cache = muxer.cache[:0]
    }
    return tags
}

func CreateVideoMuxer(cid FLV_VIDEO_CODEC_ID) AVTagMuxer {
    if cid == FLV_AVC {
        return NewAVCMuxer()
    } else if cid == FLV_HEVC {
        return NewHevcMuxer()
    }
    return nil
}

type AACMuxer struct {
    config codec.AudioSpecificConfiguration
}

func NewAACMuxer() *AACMuxer {
    return &AACMuxer{}
}

func (muxer *AACMuxer) Write(frames []byte, pts uint32, dts uint32) [][]byte {
    var tags [][]byte
    _ = muxer.WriteTags(frames, dts, func(tag []byte, _ uint32) error {
        tags = append(tags, tag)
        return nil
    })
    return tags
}

// WriteTags emits AAC tags with their DTS in milliseconds. A new audio
// configuration is saved only after output accepts its sequence header.
func (muxer *AACMuxer) WriteTags(frames []byte, dts uint32, output func(tag []byte, dts uint32) error) error {
    // This clock is divisible by every ADTS sample rate, so changing sample
    // rates within a batch does not discard fractional milliseconds.
    const aacClock = 28224000
    var elapsedTicks uint64
    for start := codec.FindSyncword(frames, 0); start >= 0; start = codec.FindSyncword(frames, 0) {
        aac := frames[start:]
        if len(aac) < 7 {
            return errors.New("incomplete AAC ADTS header")
        }
        var header codec.ADTS_Frame_Header
        header.Decode(aac)
        frameSize := int(header.Variable_Header.Frame_length)
        blocks := int(header.Variable_Header.Number_of_raw_data_blocks_in_frame) + 1
        headerSize := 7
        if header.Fix_Header.Protection_absent == 0 {
            if blocks != 1 {
                return errors.New("AAC ADTS CRC with multiple raw data blocks is unsupported")
            }
            headerSize += 2
        }
        if frameSize <= headerSize || frameSize > len(aac) {
            return errors.New("invalid AAC ADTS frame size")
        }
        asc, err := codec.ConvertADTSToASC(aac)
        if err != nil {
            return err
        }
        index := int(asc.Sample_freq_index)
        if index >= len(codec.AAC_Sampling_Idx) {
            return errors.New("invalid AAC ADTS sampling frequency")
        }
        timestamp := dts + uint32(elapsedTicks/(aacClock/1000))
        if muxer.config != *asc {
            if err := output(WriteAudioTag(asc.Encode(), FLV_AAC, 0, 0, true), timestamp); err != nil {
                return err
            }
            muxer.config = *asc
        }
        if err := output(WriteAudioTag(aac[headerSize:frameSize], FLV_AAC, 0, 0, false), timestamp); err != nil {
            return err
        }
        samples := uint64(blocks) * 1024
        elapsedTicks += samples * (aacClock / uint64(codec.AACSampleIdxToSample(index)))
        frames = aac[frameSize:]
    }
    return nil
}

type G711AMuxer struct {
    channelCount int
    sampleRate   int
}

func NewG711AMuxer(channelCount int, sampleRate int) *G711AMuxer {
    return &G711AMuxer{
        channelCount: channelCount,
        sampleRate:   sampleRate,
    }
}

func (muxer *G711AMuxer) Write(frames []byte, pts uint32, dts uint32) [][]byte {
    tags := make([][]byte, 1)
    tags[0] = WriteAudioTag(frames, FLV_G711A, muxer.sampleRate, muxer.channelCount, true)
    return tags
}

type G711UMuxer struct {
    channelCount int
    sampleRate   int
}

func NewG711UMuxer(channelCount int, sampleRate int) *G711UMuxer {
    return &G711UMuxer{
        channelCount: channelCount,
        sampleRate:   sampleRate,
    }
}

func (muxer *G711UMuxer) Write(frames []byte, pts uint32, dts uint32) [][]byte {
    tags := make([][]byte, 1)
    tags[0] = WriteAudioTag(frames, FLV_G711U, muxer.sampleRate, muxer.channelCount, true)
    return tags
}

type Mp3Muxer struct {
}

func (muxer *Mp3Muxer) Write(frames []byte, pts uint32, dts uint32) [][]byte {
    tags := make([][]byte, 1)
    codec.SplitMp3Frames(frames, func(head *codec.MP3FrameHead, frame []byte) {
        tags = append(tags, WriteAudioTag(frames, FLV_MP3, head.GetSampleRate(), head.GetChannelCount(), true))
    })
    return tags
}

func CreateAudioMuxer(cid FLV_SOUND_FORMAT) AVTagMuxer {
    if cid == FLV_AAC {
        return NewAACMuxer()
    } else if cid == FLV_G711A {
        return NewG711AMuxer(1, 5500)
    } else if cid == FLV_G711U {
        return NewG711UMuxer(1, 5500)
    } else if cid == FLV_MP3 {
        return new(Mp3Muxer)
    } else {
        return nil
    }
}

type FlvMuxer struct {
    videoMuxer AVTagMuxer
    audioMuxer AVTagMuxer
}

func NewFlvMuxer(vid FLV_VIDEO_CODEC_ID, aid FLV_SOUND_FORMAT) *FlvMuxer {
    return &FlvMuxer{
        videoMuxer: CreateVideoMuxer(vid),
        audioMuxer: CreateAudioMuxer(aid),
    }
}

func (muxer *FlvMuxer) SetVideoCodeId(cid FLV_VIDEO_CODEC_ID) {
    muxer.videoMuxer = CreateVideoMuxer(cid)
}

func (muxer *FlvMuxer) SetAudioCodeId(cid FLV_SOUND_FORMAT) {
    muxer.audioMuxer = CreateAudioMuxer(cid)
}

func (muxer *FlvMuxer) WriteVideo(frames []byte, pts uint32, dts uint32) ([][]byte, error) {
    if muxer.videoMuxer == nil {
        return nil, errors.New("video Muxer is Nil")
    }
    return muxer.WriteFrames(VIDEO_TAG, frames, pts, dts)
}

func (muxer *FlvMuxer) WriteAudio(frames []byte, pts uint32, dts uint32) ([][]byte, error) {
    if muxer.audioMuxer == nil {
        return nil, errors.New("audio Muxer is Nil")
    }
    return muxer.WriteFrames(AUDIO_TAG, frames, pts, dts)
}

func (muxer *FlvMuxer) WriteFrames(frameType TagType, frames []byte, pts uint32, dts uint32) ([][]byte, error) {
    tags := make([][]byte, 0, 1)
    err := muxer.writeFrames(frameType, frames, pts, dts, func(tag []byte) error {
        tags = append(tags, tag)
        return nil
    })
    if err != nil && len(tags) == 0 {
        return nil, err
    }
    return tags, err
}

func (muxer *FlvMuxer) writeFrames(frameType TagType, frames []byte, pts uint32, dts uint32, output func([]byte) error) error {
    writeTag := func(tag []byte, timestamp uint32) error {
        ftag := FlvTag{
            TagType: uint8(frameType),
            DataSize: uint32(len(tag)),
            Timestamp: timestamp & 0x00FFFFFF,
            TimestampExtended: uint8(timestamp >> 24),
        }
        return output(append(ftag.Encode(), tag...))
    }
    var tags [][]byte
    if frameType == AUDIO_TAG {
        if aac, ok := muxer.audioMuxer.(*AACMuxer); ok {
            return aac.WriteTags(frames, dts, writeTag)
        }
        tags = muxer.audioMuxer.Write(frames, pts, dts)
    } else if frameType == VIDEO_TAG {
        tags = muxer.videoMuxer.Write(frames, pts, dts)
    } else {
        return errors.New("unsupport Frame Type")
    }

    for _, tag := range tags {
        if err := writeTag(tag, dts); err != nil {
            return err
        }
    }
    return nil
}
