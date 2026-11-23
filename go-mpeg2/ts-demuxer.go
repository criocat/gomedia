package mpeg2

import (
    "errors"
    "io"

    "github.com/yapingcat/gomedia/go-codec"
)

type pakcet_t struct {
    payload []byte
    pts     uint64
    dts     uint64
}

func newPacket_t(size uint32) *pakcet_t {
    return &pakcet_t{
        payload: make([]byte, 0, size),
        pts:     0,
        dts:     0,
    }
}

type tsstream struct {
    cid     TS_STREAM_TYPE
    pes_sid PES_STREMA_ID
    pes_pkg *PesPacket
    pkg     *pakcet_t
    pts     uint64
    dts     uint64
    hasDTS  bool
}

const tsTimestampPeriod = uint64(1) << 33

// Place a 33-bit clock value in the epoch nearest to reference.
func unwrapTSTimestamp(value, reference uint64) uint64 {
    value |= reference &^ (tsTimestampPeriod - 1)
    if value < reference && reference-value > tsTimestampPeriod/2 {
        return value + tsTimestampPeriod
    }
    if value > reference && value-reference > tsTimestampPeriod/2 && value >= tsTimestampPeriod {
        return value - tsTimestampPeriod
    }
    return value
}

func (stream *tsstream) updateTimestamps() {
    if stream.pes_pkg.PTS_DTS_flags&2 == 0 {
        return
    }
    dts := stream.pes_pkg.Dts
    if stream.hasDTS {
        dts = unwrapTSTimestamp(dts, stream.dts)
    }
    stream.dts = dts
    stream.pts = unwrapTSTimestamp(stream.pes_pkg.Pts, dts)
    stream.hasDTS = true
}

type tsprogram struct {
    pn      uint16
    streams map[uint16]*tsstream
}

type TSDemuxer struct {
    programs   map[uint16]*tsprogram
    OnFrame    func(cid TS_STREAM_TYPE, frame []byte, pts uint64, dts uint64)
    OnTSPacket func(pkg *TSPacket)
}

func NewTSDemuxer() *TSDemuxer {
    return &TSDemuxer{
        programs:   make(map[uint16]*tsprogram),
        OnFrame:    nil,
        OnTSPacket: nil,
    }
}

func (demuxer *TSDemuxer) Input(r io.Reader) error {
    var err error = nil
    var buf []byte
    for {
        if len(buf) > TS_PAKCET_SIZE {
            buf = buf[TS_PAKCET_SIZE:]
        } else {
            if err != nil {
                if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
                    break
                }
                return err
            }
            buf, err = demuxer.probe(r)
            if err != nil && buf == nil {
                if errors.Is(err, io.EOF) {
                    break
                }
                return err
            }
        }

        bs := codec.NewBitStream(buf[:TS_PAKCET_SIZE])
        var pkg TSPacket
        if err := pkg.DecodeHeader(bs); err != nil {
            return err
        }
        if pkg.PID == uint16(TS_PID_PAT) {
            if pkg.Payload_unit_start_indicator == 1 {
                bs.SkipBits(8)
            }
            pkg.Payload, err = ReadSection(TS_TID_PAS, bs)
            if err != nil {
                return err
            }
            pat := pkg.Payload.(*Pat)
            for _, pmt := range pat.Pmts {
                if pmt.Program_number != 0x0000 {
                    if _, found := demuxer.programs[pmt.PID]; !found {
                        demuxer.programs[pmt.PID] = &tsprogram{pn: 0, streams: make(map[uint16]*tsstream)}
                    }
                }
            }
        } else if pkg.PID == TS_PID_Nil {
            continue
        } else {
            for p, s := range demuxer.programs {
                if p == pkg.PID { // pmt table
                    if pkg.Payload_unit_start_indicator == 1 {
                        bs.SkipBits(8) //pointer filed
                    }
                    pkg.Payload, err = ReadSection(TS_TID_PMS, bs)
                    if err != nil {
                        return err
                    }
                    pmt := pkg.Payload.(*Pmt)
                    if pmt.Current_next_indicator == 0 {
                        continue
                    }
                    s.pn = pmt.Program_number
                    for _, ps := range pmt.Streams {
                        cid := TS_STREAM_TYPE(ps.StreamType)
                        if old := s.streams[ps.Elementary_PID]; old != nil {
                            if old.cid == cid {
                                continue
                            }
                            demuxer.flushStream(old)
                        }
                        s.streams[ps.Elementary_PID] = &tsstream{
                            cid:     cid,
                            pes_sid: findPESIDByStreamType(cid),
                            pes_pkg: NewPesPacket(),
                        }
                    }
                } else {
                    for sid, stream := range s.streams {
                        if sid != pkg.PID {
                            continue
                        }
                        if pkg.Field != nil && pkg.Field.Discontinuity_indicator != 0 {
                            stream.hasDTS = false
                        }
                        if pkg.Payload_unit_start_indicator == 1 {
                            err := stream.pes_pkg.Decode(bs)
                            // ignore error if it was a short payload read, next ts packet should append missing data
                            if err != nil && !(errors.Is(err, errNeedMore) && stream.pes_pkg.Pes_payload != nil) {
                                return err
                            }
                            stream.updateTimestamps()
                            pkg.Payload = stream.pes_pkg
                        } else {
                            stream.pes_pkg.Pes_payload = bs.RemainData()
                            pkg.Payload = bs.RemainData()
                        }
                        if stream.pes_sid == PES_STREAM_AUDIO {
                            demuxer.doAudioPesPacket(stream, pkg.Payload_unit_start_indicator)
                        } else if stream.pes_sid == PES_STREAM_VIDEO {
                            demuxer.doVideoPesPacket(stream, pkg.Payload_unit_start_indicator)
                        }
                    }
                }
            }
        }
        if demuxer.OnTSPacket != nil {
            demuxer.OnTSPacket(&pkg)
        }
    }
    demuxer.flush()
    return nil
}

func (demuxer *TSDemuxer) probe(r io.Reader) ([]byte, error) {
    buf := make([]byte, TS_PAKCET_SIZE, 2*TS_PAKCET_SIZE)
    if _, err := io.ReadFull(r, buf); err != nil {
        return nil, err
    }
    if buf[0] == 0x47 {
        return buf, nil
    }
    buf = buf[:2*TS_PAKCET_SIZE]
    if _, err := io.ReadFull(r, buf[TS_PAKCET_SIZE:]); err != nil {
        return nil, err
    }
LOOP:
    i := 0
    for ; i < TS_PAKCET_SIZE; i++ {
        if buf[i] == 0x47 && buf[i+TS_PAKCET_SIZE] == 0x47 {
            break
        }
    }
    if i == 0 {
        return buf, nil
    } else if i < TS_PAKCET_SIZE {
        copy(buf, buf[i:])
        if _, err := io.ReadFull(r, buf[2*TS_PAKCET_SIZE-i:]); err != nil {
            return buf[:TS_PAKCET_SIZE], err
        } else {
            return buf, nil
        }
    } else {
        copy(buf, buf[TS_PAKCET_SIZE:])
        if _, err := io.ReadFull(r, buf[TS_PAKCET_SIZE:]); err != nil {
            return buf[:TS_PAKCET_SIZE], err
        }
        goto LOOP
    }
}

func (demuxer *TSDemuxer) flush() {
    for _, pm := range demuxer.programs {
        for _, stream := range pm.streams {
            demuxer.flushStream(stream)
        }
    }
}

func (demuxer *TSDemuxer) flushStream(stream *tsstream) {
    if stream.pkg == nil || len(stream.pkg.payload) == 0 || demuxer.OnFrame == nil {
        return
    }
    payload := stream.pkg.payload
    if stream.cid == TS_STREAM_H264 || stream.cid == TS_STREAM_H265 {
        audLen := 0
        codec.SplitFrameWithStartCode(payload, func(nalu []byte) bool {
            if (stream.cid == TS_STREAM_H264 && codec.H264NaluType(nalu) == codec.H264_NAL_AUD) ||
                (stream.cid == TS_STREAM_H265 && codec.H265NaluType(nalu) == codec.H265_NAL_AUD) {
                audLen += len(nalu)
            }
            return false
        })
        payload = payload[audLen:]
    }
    demuxer.OnFrame(stream.cid, payload, stream.pkg.pts/90, stream.pkg.dts/90)
    stream.pkg = nil
}

func (demuxer *TSDemuxer) doVideoPesPacket(stream *tsstream, start uint8) {
    if stream.cid != TS_STREAM_H264 && stream.cid != TS_STREAM_H265 {
        return
    }
    if stream.pkg == nil {
        stream.pkg = newPacket_t(1024)
        stream.pkg.pts = stream.pts
        stream.pkg.dts = stream.dts
    }
    stream.pkg.payload = append(stream.pkg.payload, stream.pes_pkg.Pes_payload...)
    update := false
    if stream.cid == TS_STREAM_H264 {
        update = demuxer.splitH264Frame(stream)
    } else {
        update = demuxer.splitH265Frame(stream)
    }
    if update {
        stream.pkg.pts = stream.pts
        stream.pkg.dts = stream.dts
    }
}

func (demuxer *TSDemuxer) doAudioPesPacket(stream *tsstream, start uint8) {
    if stream.cid != TS_STREAM_AAC && stream.cid != TS_STREAM_AUDIO_MPEG1 && stream.cid != TS_STREAM_AUDIO_MPEG2 {
        return
    }

    if stream.pkg == nil {
        stream.pkg = newPacket_t(1024)
        stream.pkg.pts = stream.pts
        stream.pkg.dts = stream.dts
    }

    if len(stream.pkg.payload) > 0 && (start == 1 || stream.pts != stream.pkg.pts) {
        if demuxer.OnFrame != nil {
            demuxer.OnFrame(stream.cid, stream.pkg.payload, stream.pkg.pts/90, stream.pkg.dts/90)
        }
        stream.pkg.payload = stream.pkg.payload[:0]
    }
    stream.pkg.payload = append(stream.pkg.payload, stream.pes_pkg.Pes_payload...)
    stream.pkg.pts = stream.pts
    stream.pkg.dts = stream.dts
}

func (demuxer *TSDemuxer) splitH264Frame(stream *tsstream) bool {
    data := stream.pkg.payload
    start, sct := codec.FindStartCode(data, 0)
    datalen := len(data)
    vcl := 0
    newAcessUnit := false
    needUpdate := false
    frameBeg := start
    if frameBeg < 0 {
        frameBeg = 0
    }
    for start < datalen {
        if start < 0 || len(data)-start <= int(sct)+1 {
            break
        }

        naluType := codec.H264NaluTypeWithoutStartCode(data[start+int(sct):])
        switch naluType {
        case codec.H264_NAL_AUD, codec.H264_NAL_SPS,
            codec.H264_NAL_PPS, codec.H264_NAL_SEI:
            if vcl > 0 {
                newAcessUnit = true
            }
        case codec.H264_NAL_I_SLICE, codec.H264_NAL_P_SLICE,
            codec.H264_NAL_SLICE_A, codec.H264_NAL_SLICE_B, codec.H264_NAL_SLICE_C:
            if vcl > 0 {
                // bs := codec.NewBitStream(data[start+int(sct)+1:])
                // sliceHdr := &codec.SliceHeader{}
                // sliceHdr.Decode(bs)
                if data[start+int(sct)+1]&0x80 > 0 {
                    newAcessUnit = true
                }
            } else {
                vcl++
            }
        }

        if vcl > 0 && newAcessUnit {
            if demuxer.OnFrame != nil {
                audLen := 0
                codec.SplitFrameWithStartCode(data[frameBeg:start], func(nalu []byte) bool {
                    if codec.H264NaluType(nalu) == codec.H264_NAL_AUD {
                        audLen += len(nalu)
                    }
                    return false
                })
                demuxer.OnFrame(stream.cid, data[frameBeg+audLen:start], stream.pkg.pts/90, stream.pkg.dts/90)
            }
            frameBeg = start
            needUpdate = true
            vcl = 0
            newAcessUnit = false
        }
        end, sct2 := codec.FindStartCode(data, start+3)
        if end < 0 {
            break
        }
        start = end
        sct = sct2
    }

    if frameBeg == 0 {
        return needUpdate
    }
    copy(stream.pkg.payload, data[frameBeg:datalen])
    stream.pkg.payload = stream.pkg.payload[0 : datalen-frameBeg]
    return needUpdate
}

func (demuxer *TSDemuxer) splitH265Frame(stream *tsstream) bool {
    data := stream.pkg.payload
    start, sct := codec.FindStartCode(data, 0)
    datalen := len(data)
    vcl := 0
    newAcessUnit := false
    needUpdate := false
    frameBeg := start
    if frameBeg < 0 {
        frameBeg = 0
    }
    for start < datalen {
        if start < 0 || len(data)-start <= int(sct)+2 {
            break
        }
        naluType := codec.H265NaluTypeWithoutStartCode(data[start+int(sct):])
        switch naluType {
        case codec.H265_NAL_AUD, codec.H265_NAL_SPS,
            codec.H265_NAL_PPS, codec.H265_NAL_VPS, codec.H265_NAL_SEI:
            if vcl > 0 {
                newAcessUnit = true
            }
        case codec.H265_NAL_Slice_TRAIL_N, codec.H265_NAL_LICE_TRAIL_R,
            codec.H265_NAL_SLICE_TSA_N, codec.H265_NAL_SLICE_TSA_R,
            codec.H265_NAL_SLICE_STSA_N, codec.H265_NAL_SLICE_STSA_R,
            codec.H265_NAL_SLICE_RADL_N, codec.H265_NAL_SLICE_RADL_R,
            codec.H265_NAL_SLICE_RASL_N, codec.H265_NAL_SLICE_RASL_R,
            codec.H265_NAL_SLICE_BLA_W_LP, codec.H265_NAL_SLICE_BLA_W_RADL,
            codec.H265_NAL_SLICE_BLA_N_LP, codec.H265_NAL_SLICE_IDR_W_RADL,
            codec.H265_NAL_SLICE_IDR_N_LP, codec.H265_NAL_SLICE_CRA:
            if vcl > 0 {
                // bs := codec.NewBitStream(data[start+int(sct)+2:])
                // sliceHdr := &codec.SliceHeader{}
                // sliceHdr.Decode(bs)
                // if sliceHdr.First_mb_in_slice == 0 {
                //     newAcessUnit = true
                // }
                if data[start+int(sct)+2]&0x80 > 0 {
                    newAcessUnit = true
                }
            } else {
                vcl++
            }
        }

        if vcl > 0 && newAcessUnit {
            if demuxer.OnFrame != nil {
                audLen := 0
                codec.SplitFrameWithStartCode(data[frameBeg:start], func(nalu []byte) bool {
                    if codec.H265NaluType(nalu) == codec.H265_NAL_AUD {
                        audLen = len(nalu)
                    }
                    return false
                })
                demuxer.OnFrame(stream.cid, data[frameBeg+audLen:start], stream.pkg.pts/90, stream.pkg.dts/90)
            }
            frameBeg = start
            needUpdate = true
            vcl = 0
            newAcessUnit = false
        }

        end, sct2 := codec.FindStartCode(data, start+3)
        if end < 0 {
            break
        }
        start = end
        sct = sct2
    }

    if frameBeg == 0 {
        return needUpdate
    }
    copy(stream.pkg.payload, data[frameBeg:datalen])
    stream.pkg.payload = stream.pkg.payload[0 : datalen-frameBeg]
    return needUpdate
}
