package mp4

import (
    "bytes"
    "encoding/binary"
    "errors"
    "io"

    "github.com/yapingcat/gomedia/go-codec"
)

type sampleCache struct {
    pts    uint64
    dts    uint64
    hasVcl bool
    isKey  bool
    cache  []byte
}

type sampleEntry struct {
    pts                    uint64
    dts                    uint64
    offset                 uint64
    size                   uint64
    isKeyFrame             bool
    SampleDescriptionIndex uint32 //always should be 1
}

type movchunk struct {
    chunknum    uint32
    samplenum   uint32
    chunkoffset uint64
}

type extraData interface {
    export() []byte
    load(data []byte)
}

type videoParameterSetState struct {
    sealed        bool
    inBand        bool
    updated       bool
    pendingUpdate bool
}

func (state *videoParameterSetState) markUpdate(changed bool) {
    if changed && state.sealed {
        state.updated = true
        state.pendingUpdate = true
    }
}

func (state *videoParameterSetState) writeInBand(keyFrame bool) bool {
    if !state.pendingUpdate && !(state.updated && keyFrame) {
        return false
    }
    state.pendingUpdate = false
    state.inBand = true
    return true
}

type h264ExtraData struct {
    videoParameterSetState
    spss          [][]byte
    ppss          [][]byte
    nalLengthSize int
}

func (extra *h264ExtraData) export() []byte {
    spss := append([][]byte(nil), extra.spss...)
    ppss := append([][]byte(nil), extra.ppss...)
    data, _ := codec.CreateH264AVCCExtradata(spss, ppss)
    return data
}

func (extra *h264ExtraData) load(data []byte) {
    extra.spss, extra.ppss = codec.CovertExtradata(data)
    if len(data) > 4 {
        extra.nalLengthSize = int(data[4]&3) + 1
    }
}

type h265ExtraData struct {
    videoParameterSetState
    hvccExtra *codec.HEVCRecordConfiguration
}

func newh265ExtraData() *h265ExtraData {
    return &h265ExtraData{
        hvccExtra: codec.NewHEVCRecordConfiguration(),
    }
}

func (extra *h265ExtraData) export() []byte {
    if extra.hvccExtra == nil {
        panic("extra.hvccExtra must init")
    }
    data, _ := extra.hvccExtra.Encode()
    return data
}

func (extra *h265ExtraData) load(data []byte) {
    if extra.hvccExtra == nil {
        panic("extra.hvccExtra must init")
    }
    extra.hvccExtra.Decode(data)
}

type aacExtraData struct {
    asc []byte
}

func (extra *aacExtraData) export() []byte {
    return extra.asc
}

func (extra *aacExtraData) load(data []byte) {
    extra.asc = make([]byte, len(data))
    copy(extra.asc, data)
}

type movFragment struct {
    offset   uint64
    duration uint32
    firstDts uint64
    firstPts uint64
    lastPts  uint64
    lastDts  uint64
}

type mp4track struct {
    cid         MP4_CODEC_TYPE
    trackId     uint32
    stbltable   *movstbl
    duration    uint32
    timescale   uint32
    width       uint32
    height      uint32
    sampleRate  uint32
    sampleBits  uint8
    chanelCount uint8
    samplelist  []sampleEntry
    elst        *movelst
    extra       extraData
    lastSample  *sampleCache
    writer      io.WriteSeeker
    fragments   []movFragment

    //for fmp4
    extraData          []byte
    startDts           uint64
    startPts           uint64
    defaultSize        uint32
    defaultDuration    uint32
    defaultSampleFlags uint32
    baseDataOffset     uint64

	//for subsample
	defaultIsProtected     uint8
	defaultPerSampleIVSize uint8
	defaultCryptByteBlock  uint8
	defaultSkipByteBlock   uint8
	defaultConstantIV      []byte
	defaultKID             [16]byte
	lastSeig               *SeigSampleGroupEntry
	lastSaiz 			   *SaizBox
	subSamples             []sencEntry
}

func newmp4track(cid MP4_CODEC_TYPE, writer io.WriteSeeker) *mp4track {
    track := &mp4track{
        cid:        cid,
        timescale:  1000,
        stbltable:  nil,
        samplelist: make([]sampleEntry, 0),
        lastSample: &sampleCache{
            hasVcl: false,
            cache:  make([]byte, 0, 128),
        },
        writer:    writer,
        fragments: make([]movFragment, 0, 32),
        startDts:  0,
    }

    if cid == MP4_CODEC_H264 {
        track.extra = new(h264ExtraData)
    } else if cid == MP4_CODEC_H265 {
        track.extra = newh265ExtraData()
    } else if cid == MP4_CODEC_AAC {
        track.extra = new(aacExtraData)
    }
    return track
}

func (track *mp4track) addSampleEntry(entry sampleEntry) {
    if len(track.samplelist) <= 1 {
        track.duration = 0
    } else {
        previousDTS := track.samplelist[len(track.samplelist)-1].dts
        if entry.dts < previousDTS {
            decrease := previousDTS - entry.dts
            if decrease > uint64(track.duration) {
                track.duration = 0
            } else {
                track.duration -= uint32(decrease)
            }
        } else {
            track.duration += uint32(entry.dts - previousDTS)
        }
    }
    track.samplelist = append(track.samplelist, entry)
}

func (track *mp4track) makeStblTable() {
    if track.stbltable == nil {
        track.stbltable = new(movstbl)
    }
    sameSize := true
    stts := new(movstts)
    stts.entrys = make([]sttsEntry, 0)
    movchunks := make([]movchunk, 0)
    ctts := new(movctts)
    ctts.entrys = make([]cttsEntry, 0)
    ckn := uint32(0)
    for i, sample := range track.samplelist {
        sttsEntry := sttsEntry{sampleCount: 1, sampleDelta: 1}
        cttsEntry := cttsEntry{sampleCount: 1, sampleOffset: uint32(sample.pts) - uint32(sample.dts)}
        if i == len(track.samplelist)-1 {
            stts.entrys = append(stts.entrys, sttsEntry)
            stts.entryCount++
        } else {
            var delta uint64 = 1
            if track.samplelist[i+1].dts >= sample.dts {
                delta = track.samplelist[i+1].dts - sample.dts
            }

            if len(stts.entrys) > 0 && delta == uint64(stts.entrys[len(stts.entrys)-1].sampleDelta) {
                stts.entrys[len(stts.entrys)-1].sampleCount++
            } else {
                sttsEntry.sampleDelta = uint32(delta)
                stts.entrys = append(stts.entrys, sttsEntry)
                stts.entryCount++
            }
        }

        if len(ctts.entrys) == 0 {
            ctts.entrys = append(ctts.entrys, cttsEntry)
            ctts.entryCount++
        } else {
            if ctts.entrys[len(ctts.entrys)-1].sampleOffset == cttsEntry.sampleOffset {
                ctts.entrys[len(ctts.entrys)-1].sampleCount++
            } else {
                ctts.entrys = append(ctts.entrys, cttsEntry)
                ctts.entryCount++
            }
        }
        if sameSize && i < len(track.samplelist)-1 && track.samplelist[i+1].size != track.samplelist[i].size {
            sameSize = false
        }
        if i > 0 && sample.offset == track.samplelist[i-1].offset+track.samplelist[i-1].size {
            movchunks[ckn-1].samplenum++
        } else {
            ck := movchunk{chunknum: ckn, samplenum: 1, chunkoffset: sample.offset}
            movchunks = append(movchunks, ck)
            ckn++
        }
    }
    stsz := &movstsz{
        sampleSize:  0,
        sampleCount: uint32(len(track.samplelist)),
    }
    if sameSize {
        stsz.sampleSize = uint32(track.samplelist[0].size)
    } else {
        stsz.entrySizelist = make([]uint32, stsz.sampleCount)
        for i := 0; i < len(stsz.entrySizelist); i++ {
            stsz.entrySizelist[i] = uint32(track.samplelist[i].size)
        }
    }

    stsc := &movstsc{
        entrys:     make([]stscEntry, len(movchunks)),
        entryCount: 0,
    }
    for i, chunk := range movchunks {
        if i == 0 || chunk.samplenum != movchunks[i-1].samplenum {
            stsc.entrys[stsc.entryCount].firstChunk = chunk.chunknum + 1
            stsc.entrys[stsc.entryCount].sampleDescriptionIndex = 1
            stsc.entrys[stsc.entryCount].samplesPerChunk = chunk.samplenum
            stsc.entryCount++
        }
    }
    stco := &movstco{entryCount: ckn, chunkOffsetlist: make([]uint64, ckn)}
    for i := 0; i < int(stco.entryCount); i++ {
        stco.chunkOffsetlist[i] = movchunks[i].chunkoffset
    }
    track.stbltable.stts = stts
    track.stbltable.stsc = stsc
    track.stbltable.stco = stco
    track.stbltable.stsz = stsz
    if track.cid == MP4_CODEC_H264 || track.cid == MP4_CODEC_H265 {
        track.stbltable.ctts = ctts
    }
}

func (track *mp4track) makeEmptyStblTable() {
    track.stbltable = new(movstbl)
    track.stbltable.stts = &movstts{}
    track.stbltable.stsc = &movstsc{}
    track.stbltable.stco = &movstco{}
    track.stbltable.stsz = &movstsz{}
    track.stbltable.stss = &movstss{}
}

func (track *mp4track) writeSample(sample []byte, pts, dts uint64) (err error) {
    switch track.cid {
    case MP4_CODEC_H264:
        err = track.writeH264(sample, pts, dts)
    case MP4_CODEC_H265:
        err = track.writeH265(sample, pts, dts)
    case MP4_CODEC_AAC:
        err = track.writeAAC(sample, pts, dts)
    case MP4_CODEC_G711A, MP4_CODEC_G711U:
        err = track.writeAudioSample(sample, pts, dts)
    case MP4_CODEC_MP2, MP4_CODEC_MP3:
        err = track.writeMP3(sample, pts, dts)
    case MP4_CODEC_OPUS:
        err = track.writeOPUS(sample, pts, dts)
    }
    return err
}

func (track *mp4track) videoParameterSets() *videoParameterSetState {
    switch extra := track.extra.(type) {
    case *h264ExtraData:
        return &extra.videoParameterSetState
    case *h265ExtraData:
        return &extra.videoParameterSetState
    default:
        return nil
    }
}

func (track *mp4track) sealVideoConfiguration(state *videoParameterSetState) {
    if !state.sealed {
        if len(track.extraData) == 0 {
            track.extraData = track.extra.export()
        }
        state.sealed = true
    }
}

func updateH264ParameterSet(sets *[][]byte, nalu []byte, getID func([]byte) uint64) bool {
    start, length := codec.FindStartCode(nalu, 0)
    payload := nalu[start+int(length):]
    id := getID(payload)
    for i, set := range *sets {
        if getID(set[4:]) != id {
            continue
        }
        if bytes.Equal(set[4:], payload) {
            return false
        }
        (*sets)[i] = append([]byte{0, 0, 0, 1}, payload...)
        return true
    }
    *sets = append(*sets, append([]byte{0, 0, 0, 1}, payload...))
    return true
}

func (track *mp4track) writeH264(h264 []byte, pts, dts uint64) (err error) {
    extra := track.extra.(*h264ExtraData)
    codec.SplitFrameWithStartCode(h264, func(nalu []byte) bool {
        nalType := codec.H264NaluType(nalu)
        sample := track.lastSample
        if sample.hasVcl && isH264NewAccessUnit(nalu) {
            if err = track.flush(); err != nil {
                return false
            }
        }

        var changed bool
        switch nalType {
        case codec.H264_NAL_SPS:
            changed = updateH264ParameterSet(&extra.spss, nalu, codec.GetSPSId)
            if track.width == 0 || track.height == 0 {
                width, height := codec.GetH264Resolution(nalu)
                if track.width == 0 {
                    track.width = width
                }
                if track.height == 0 {
                    track.height = height
                }
            }
        case codec.H264_NAL_PPS:
            changed = updateH264ParameterSet(&extra.ppss, nalu, codec.GetPPSId)
        default:
            if codec.IsH264VCLNaluType(nalType) {
                if !sample.hasVcl {
                    track.sealVideoConfiguration(&extra.videoParameterSetState)
                    if extra.writeInBand(nalType == codec.H264_NAL_I_SLICE) {
                        // Put the active configuration ahead of any cached SEI and VCL data.
                        var configuration []byte
                        for _, sets := range [][][]byte{extra.spss, extra.ppss} {
                            for _, set := range sets {
                                configuration = appendMP4NAL(configuration, set)
                            }
                        }
                        sample.cache = append(configuration, sample.cache...)
                    }
                }
                sample.pts = pts
                sample.dts = dts
                sample.hasVcl = true
                sample.isKey = nalType == codec.H264_NAL_I_SLICE
            }
            sample.cache = appendMP4NAL(sample.cache, nalu)
            return true
        }

        extra.markUpdate(changed)
        return true
    })
    return
}

func appendMP4NAL(dst, nalu []byte) []byte {
    start, length := codec.FindStartCode(nalu, 0)
    return appendMP4NALPayload(dst, nalu[start+int(length):])
}

func appendMP4NALPayload(dst, payload []byte) []byte {
    var size [4]byte
    binary.BigEndian.PutUint32(size[:], uint32(len(payload)))
    dst = append(dst, size[:]...)
    return append(dst, payload...)
}

func (extra *h265ExtraData) updateParameterSet(nalu []byte, nalType codec.H265_NAL_TYPE, update func([]byte)) {
    start, length := codec.FindStartCode(nalu, 0)
    payload := nalu[start+int(length):]
    for _, array := range extra.hvccExtra.Arrays {
        if array.NAL_unit_type == uint8(nalType) {
            for _, unit := range array.NalUnits {
                if bytes.Equal(unit.Nalu, payload) {
                    return
                }
            }
            break
        }
    }
    update(nalu)
    extra.markUpdate(true)
}

func (extra *h265ExtraData) parameterSetNALs() []byte {
    var config []byte
    for _, nalType := range [...]codec.H265_NAL_TYPE{codec.H265_NAL_VPS, codec.H265_NAL_SPS, codec.H265_NAL_PPS} {
        for _, array := range extra.hvccExtra.Arrays {
            if array.NAL_unit_type == uint8(nalType) {
                for _, unit := range array.NalUnits {
                    config = appendMP4NALPayload(config, unit.Nalu)
                }
            }
        }
    }
    return config
}

func (track *mp4track) writeH265(h265 []byte, pts, dts uint64) (err error) {
    extra := track.extra.(*h265ExtraData)
    codec.SplitFrameWithStartCode(h265, func(nalu []byte) bool {
        nalType := codec.H265NaluType(nalu)
        sample := track.lastSample
        if sample.hasVcl && isH265NewAccessUnit(nalu) {
            if err = track.flush(); err != nil {
                return false
            }
        }

        switch nalType {
        case codec.H265_NAL_VPS:
            extra.updateParameterSet(nalu, nalType, extra.hvccExtra.UpdateVPS)
        case codec.H265_NAL_SPS:
            extra.updateParameterSet(nalu, nalType, extra.hvccExtra.UpdateSPS)
            if track.width == 0 || track.height == 0 {
                width, height := codec.GetH265Resolution(nalu)
                if track.width == 0 {
                    track.width = width
                }
                if track.height == 0 {
                    track.height = height
                }
            }
        case codec.H265_NAL_PPS:
            extra.updateParameterSet(nalu, nalType, extra.hvccExtra.UpdatePPS)
        default:
            if codec.IsH265VCLNaluType(nalType) {
                keyFrame := nalType >= codec.H265_NAL_SLICE_BLA_W_LP && nalType <= codec.H265_NAL_SLICE_CRA
                if !sample.hasVcl {
                    track.sealVideoConfiguration(&extra.videoParameterSetState)
                    if extra.writeInBand(keyFrame) {
                        sample.cache = append(extra.parameterSetNALs(), sample.cache...)
                    }
                }
                sample.pts = pts
                sample.dts = dts
                sample.hasVcl = true
                sample.isKey = keyFrame
            }
            sample.cache = appendMP4NAL(sample.cache, nalu)
        }
        return true
    })
    return
}

func (track *mp4track) writeAAC(aacframes []byte, pts, dts uint64) error {
    aacextra, ok := track.extra.(*aacExtraData)
    if !ok {
        return errors.New("must init aacExtraData first")
    }
    var elapsedSamples, sampleRate uint64
    for len(aacframes) > 0 {
        if bytes.HasPrefix(aacframes, []byte("ID3")) {
            tagSize, err := id3v2TagSize(aacframes)
            if err != nil {
                return err
            }
            aacframes = aacframes[tagSize:]
            continue
        }
        if len(aacframes) < 7 || codec.FindSyncword(aacframes[:2], 0) != 0 {
            return errors.New("invalid AAC ADTS header")
        }
        var header codec.ADTS_Frame_Header
        header.Decode(aacframes)
        frameSize := int(header.Variable_Header.Frame_length)
        headerSize := 7
        if header.Fix_Header.Protection_absent == 0 {
            headerSize = 9
        }
        if frameSize <= headerSize || frameSize > len(aacframes) {
            return errors.New("invalid AAC ADTS frame size")
        }
        rateIndex := int(header.Fix_Header.Sampling_frequency_index)
        if rateIndex >= len(codec.AAC_Sampling_Idx) {
            return errors.New("invalid AAC ADTS sampling frequency")
        }
        rate := uint64(codec.AACSampleIdxToSample(rateIndex))
        if sampleRate != 0 && rate != sampleRate {
            return errors.New("AAC sampling frequency changed within a batch")
        }
        sampleRate = rate
        offset := elapsedSamples * uint64(track.timescale) / sampleRate
        if err := track.writeAudioSample(aacframes[headerSize:frameSize], pts+offset, dts+offset); err != nil {
            return err
        }
        if len(aacextra.asc) == 0 {
            asc, _ := codec.ConvertADTSToASC(aacframes[:headerSize])
            aacextra.asc = asc.Encode()
            if track.chanelCount == 0 {
                track.chanelCount = asc.Channel_configuration
                if asc.Channel_configuration == 7 {
                    track.chanelCount = 8 // ADTS configuration 7 is 7.1.
                }
            }
            if track.sampleRate == 0 {
                track.sampleRate = uint32(sampleRate)
            }
            if track.sampleBits == 0 {
                track.sampleBits = 16
            }
        }
        elapsedSamples += (uint64(header.Variable_Header.Number_of_raw_data_blocks_in_frame) + 1) * 1024
        aacframes = aacframes[frameSize:]
    }
    return nil
}

func id3v2TagSize(data []byte) (int, error) {
    const headerSize = 10
    if len(data) < headerSize || data[3] < 2 || data[3] > 4 {
        return 0, errors.New("invalid ID3v2 header")
    }
    // ID3v2 uses four synchsafe bytes for the body size.
    size := 0
    for _, octet := range data[6:headerSize] {
        if octet&0x80 != 0 {
            return 0, errors.New("invalid ID3v2 size")
        }
        size = size<<7 | int(octet)
    }
    size += headerSize
    if data[3] == 4 && data[5]&0x10 != 0 {
        size += headerSize
    }
    if size > len(data) {
        return 0, io.ErrUnexpectedEOF
    }
    return size, nil
}

func (track *mp4track) writeAudioSample(data []byte, pts, dts uint64) (err error) {
    var currentOffset int64
    if currentOffset, err = track.writer.Seek(0, io.SeekCurrent); err != nil {
        return
    }
    entry := sampleEntry{
        pts:                    pts,
        dts:                    dts,
        size:                   0,
        SampleDescriptionIndex: 1,
        offset:                 uint64(currentOffset),
    }
    n := 0
    n, err = track.writer.Write(data)
    if err != nil {
        return err
    }
    if n != len(data) {
        return io.ErrShortWrite
    }
    entry.size = uint64(n)
    track.addSampleEntry(entry)
    return
}

func (track *mp4track) writeMP3(mp3 []byte, pts, dts uint64) (err error) {
    if track.sampleRate == 0 {
        codec.SplitMp3Frames(mp3, func(head *codec.MP3FrameHead, frame []byte) {
            track.sampleRate = uint32(head.GetSampleRate())
            track.chanelCount = uint8(head.GetChannelCount())
            track.sampleBits = 16
        })
        if track.sampleRate > 24000 {
            track.cid = MP4_CODEC_MP2
        } else {
            track.cid = MP4_CODEC_MP3
        }
    }
    return track.writeAudioSample(mp3, pts, dts)
}

func (track *mp4track) writeOPUS(opus []byte, pts, dts uint64) error {
    if len(opus) == 0 {
        return errors.New("empty Opus packet")
    }
    if len(track.samplelist) > 0 {
        return track.writeAudioSample(opus, pts, dts)
    }
    extraData := track.extraData
    if len(extraData) == 0 {
        channels := track.chanelCount
        if channels == 0 {
            channels = 1 + ((opus[0] >> 2) & 1)
        }
        if channels > 2 {
            return errors.New("multichannel Opus requires OpusHead")
        }
        context := codec.OpusContext{ChannelCount: int(channels), SampleRate: 48000}
        extraData = context.WriteOpusExtraData()
    }
    config, _, err := opusSpecificBoxFromHead(extraData)
    if err != nil {
        return err
    }
    if err := track.writeAudioSample(opus, pts, dts); err != nil {
        return err
    }
    track.extraData = extraData
    track.chanelCount = config.OutputChannelCount
    track.sampleRate = 48000
    track.sampleBits = 16
    return nil
}

func (track *mp4track) flush() (err error) {
    var currentOffset int64
    if track.lastSample != nil && len(track.lastSample.cache) > 0 {
        if currentOffset, err = track.writer.Seek(0, io.SeekCurrent); err != nil {
            return err
        }
        entry := sampleEntry{
            pts:                    track.lastSample.pts,
            dts:                    track.lastSample.dts,
            isKeyFrame:             track.lastSample.isKey,
            size:                   0,
            SampleDescriptionIndex: 1,
            offset:                 uint64(currentOffset),
        }
        n := 0
        if n, err = track.writer.Write(track.lastSample.cache); err != nil {
            return err
        }
        entry.size = uint64(n)
        track.addSampleEntry(entry)
        track.lastSample.cache = track.lastSample.cache[:0]
        track.lastSample.hasVcl = false
        track.lastSample.isKey = false
        track.lastSample.dts = 0
        track.lastSample.pts = 0
    }
    return nil
}

func (track *mp4track) clearSamples() {
    track.samplelist = track.samplelist[:0]
}
