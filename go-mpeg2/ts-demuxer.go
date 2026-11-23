package mpeg2

import (
    "errors"
    "io"

    "github.com/yapingcat/gomedia/go-codec"
)

type pakcet_t struct {
    payload         []byte
    pts             uint64
    dts             uint64
    videoTimestamps pesVideoTimestamps
}

func newPacket_t(size uint32) *pakcet_t {
    return &pakcet_t{
        payload: make([]byte, 0, size),
        pts:     0,
        dts:     0,
    }
}

type tsstream struct {
    cid           TS_STREAM_TYPE
    pes_sid       PES_STREMA_ID
    pes_pkg       *PesPacket
    pkg           *pakcet_t
    pts           uint64
    dts           uint64
    hasDTS        bool
    continuity    uint8
    hasContinuity bool
    inPES         bool
    pesHeader     []byte
    pesBytesLeft  int // -1 when the PES packet length is unspecified.
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
    pn                uint16
    transportStreamID uint16
    streams           map[uint16]*tsstream
    sections          psiSections
}

type TSDemuxer struct {
    programs    map[uint16]*tsprogram
    patSections psiSections
    pat         patAssembly
    OnFrame     func(cid TS_STREAM_TYPE, frame []byte, pts uint64, dts uint64)
    OnTSPacket  func(pkg *TSPacket)
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
            if err := demuxer.readPAT(&pkg, bs.RemainData()); err != nil {
                return err
            }
        } else if pkg.PID == TS_PID_Nil {
            continue
        } else {
            for p, s := range demuxer.programs {
                if p == pkg.PID { // pmt table
                    if err := demuxer.readPMT(s, &pkg, bs.RemainData()); err != nil {
                        return err
                    }
                } else if stream := s.streams[pkg.PID]; stream != nil {
                    if err := demuxer.readPES(stream, &pkg, bs.RemainData()); err != nil {
                        return err
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

func (demuxer *TSDemuxer) readPAT(packet *TSPacket, data []byte) error {
    if packet.Field != nil && packet.Field.Discontinuity_indicator != 0 {
        demuxer.pat = patAssembly{}
    }
    return demuxer.patSections.input(packet, data, func(section []byte) error {
        pat := NewPat()
        if err := pat.Decode(codec.NewBitStream(section)); err != nil {
            return err
        }
        if packet.Payload == nil {
            packet.Payload = pat
        }
        if pat.Current_next_indicator == 0 {
            return nil
        }
        if pat.Section_number > pat.Last_section_number {
            return errors.New("MPEG-TS PAT section number exceeds the last section")
        }
        if pairs, complete := demuxer.pat.add(pat); complete {
            demuxer.applyPAT(pat.Transport_stream_id, pairs)
        }
        return nil
    })
}

func (demuxer *TSDemuxer) applyPAT(transportStreamID uint16, pairs []PmtPair) {
    programs := make(map[uint16]*tsprogram, len(pairs))
    for _, pair := range pairs {
        if pair.Program_number == 0 {
            continue
        }
        program := demuxer.programs[pair.PID]
        if program == nil || program.pn != pair.Program_number || program.transportStreamID != transportStreamID {
            program = &tsprogram{
                pn:                pair.Program_number,
                transportStreamID: transportStreamID,
                streams:           make(map[uint16]*tsstream),
            }
        }
        programs[pair.PID] = program
    }
    for pid, program := range demuxer.programs {
        if programs[pid] != program {
            for _, stream := range program.streams {
                demuxer.flushStream(stream)
            }
        }
    }
    demuxer.programs = programs
}

func (stream *tsstream) dropIncompletePES() {
    if stream.pesBytesLeft != 0 {
        stream.pkg = nil
    }
    stream.inPES = false
    stream.pesHeader = nil
    stream.pesBytesLeft = 0
}

func (demuxer *TSDemuxer) readPES(stream *tsstream, packet *TSPacket, payload []byte) error {
    if packet.Field != nil && packet.Field.Discontinuity_indicator != 0 {
        stream.hasDTS = false
        stream.hasContinuity = false
        stream.dropIncompletePES()
    }
    if packet.Adaptation_field_control&1 == 0 {
        return nil
    }
    if stream.hasContinuity {
        if packet.Continuity_counter == stream.continuity {
            return nil
        }
        if packet.Continuity_counter != (stream.continuity+1)&0x0f {
            stream.dropIncompletePES()
        }
    }
    stream.continuity = packet.Continuity_counter
    stream.hasContinuity = true

    start := packet.Payload_unit_start_indicator
    if start == 1 {
        if stream.pesBytesLeft > 0 {
            stream.dropIncompletePES()
        }
        stream.inPES = false
        stream.pesHeader = nil
        stream.pesBytesLeft = 0
    } else if len(stream.pesHeader) > 0 {
        payload = append(stream.pesHeader, payload...)
        start = 1
    }

    pes := stream.pes_pkg
    if start == 1 {
        pes.Pes_payload = nil
        err := pes.Decode(codec.NewBitStream(payload))
        if err != nil {
            if !errors.Is(err, errNeedMore) {
                return err
            }
            if pes.Pes_payload == nil {
                stream.pesHeader = append(stream.pesHeader[:0], payload...)
                return nil
            }
        }
        stream.pesHeader = nil
        stream.inPES = true
        stream.pesBytesLeft = -1
        if pes.PES_packet_length != 0 {
            stream.pesBytesLeft = int(pes.PES_packet_length) - 3 - int(pes.PES_header_data_length) - len(pes.Pes_payload)
            if stream.pesBytesLeft < 0 {
                return errors.New("invalid MPEG-TS PES packet length")
            }
        }
        stream.updateTimestamps()
        packet.Payload = pes
    } else {
        if !stream.inPES || stream.pesBytesLeft == 0 {
            return nil
        }
        if stream.pesBytesLeft > 0 {
            if len(payload) > stream.pesBytesLeft {
                payload = payload[:stream.pesBytesLeft]
            }
            stream.pesBytesLeft -= len(payload)
        }
        pes.Pes_payload = payload
        packet.Payload = payload
    }
    if stream.pes_sid == PES_STREAM_AUDIO {
        demuxer.doAudioPesPacket(stream, start)
    } else if stream.pes_sid == PES_STREAM_VIDEO {
        demuxer.doVideoPesPacket(stream, start == 1 && pes.PTS_DTS_flags&2 != 0)
    }
    return nil
}

func (demuxer *TSDemuxer) readPMT(program *tsprogram, packet *TSPacket, data []byte) error {
    return program.sections.input(packet, data, func(section []byte) error {
        pmt := NewPmt()
        if err := pmt.Decode(codec.NewBitStream(section)); err != nil {
            return err
        }
        if packet.Payload == nil {
            packet.Payload = pmt
        }
        if pmt.Current_next_indicator == 0 {
            return nil
        }
        program.pn = pmt.Program_number
        streams := make(map[uint16]*tsstream, len(pmt.Streams))
        for _, entry := range pmt.Streams {
            cid := TS_STREAM_TYPE(entry.StreamType)
            if old := program.streams[entry.Elementary_PID]; old != nil && old.cid == cid {
                streams[entry.Elementary_PID] = old
                continue
            }
            streams[entry.Elementary_PID] = &tsstream{
                cid:     cid,
                pes_sid: findPESIDByStreamType(cid),
                pes_pkg: NewPesPacket(),
            }
        }
        for pid, stream := range program.streams {
            if streams[pid] != stream {
                demuxer.flushStream(stream)
            }
        }
        program.streams = streams
        return nil
    })
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
    for {
        n, err := io.ReadFull(r, buf[TS_PAKCET_SIZE:])
        if err != nil {
            if (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) && buf[n] == 0x47 {
                // The last complete packet ends at EOF, so a second sync
                // byte is unavailable to confirm alignment.
                return buf[n : n+TS_PAKCET_SIZE], err
            }
            return nil, err
        }

        i := 0
        for ; i < TS_PAKCET_SIZE; i++ {
            if buf[i] == 0x47 && buf[i+TS_PAKCET_SIZE] == 0x47 {
                break
            }
        }
        if i == 0 {
            return buf, nil
        }
        if i < TS_PAKCET_SIZE {
            copy(buf, buf[i:])
            if _, err := io.ReadFull(r, buf[2*TS_PAKCET_SIZE-i:]); err != nil {
                return buf[:TS_PAKCET_SIZE], err
            }
            return buf, nil
        }
        copy(buf, buf[TS_PAKCET_SIZE:])
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
    if stream.pkg == nil || len(stream.pkg.payload) == 0 || stream.pesBytesLeft > 0 || demuxer.OnFrame == nil {
        return
    }
    payload := stream.pkg.payload
    if stream.cid == TS_STREAM_H264 || stream.cid == TS_STREAM_H265 {
        payload = stripLeadingVideoAUD(stream.cid, payload)
    }
    if len(payload) != 0 {
        demuxer.OnFrame(stream.cid, payload, stream.pkg.pts/90, stream.pkg.dts/90)
    }
    stream.pkg = nil
}

func stripLeadingVideoAUD(cid TS_STREAM_TYPE, frame []byte) []byte {
    audLen := 0
    codec.SplitFrameWithStartCode(frame, func(nalu []byte) bool {
        if (cid == TS_STREAM_H264 && codec.H264NaluType(nalu) == codec.H264_NAL_AUD) ||
            (cid == TS_STREAM_H265 && codec.H265NaluType(nalu) == codec.H265_NAL_AUD) {
            audLen = len(nalu)
        }
        return false
    })
    return frame[audLen:]
}

func (demuxer *TSDemuxer) doVideoPesPacket(stream *tsstream, hasTimestamp bool) {
    if stream.cid != TS_STREAM_H264 && stream.cid != TS_STREAM_H265 {
        return
    }
    if stream.pkg == nil {
        stream.pkg = newPacket_t(1024)
        stream.pkg.pts = stream.pts
        stream.pkg.dts = stream.dts
    }
    if hasTimestamp {
        stream.pkg.videoTimestamps = append(stream.pkg.videoTimestamps, pesVideoTimestamp{
            offset: len(stream.pkg.payload),
            pts:    stream.pts,
            dts:    stream.dts,
        })
    }
    stream.pkg.payload = append(stream.pkg.payload, stream.pes_pkg.Pes_payload...)
    demuxer.splitVideoAccessUnits(stream)
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

func (demuxer *TSDemuxer) splitVideoAccessUnits(stream *tsstream) {
    data := stream.pkg.payload
    start, startCodeSize := codec.FindStartCode(data, 0)
    frameStart := start
    if frameStart >= 0 {
        stream.pkg.videoTimestamps.applyThrough(frameStart, &stream.pkg.pts, &stream.pkg.dts)
    }
    headerSize := 1
    if stream.cid == TS_STREAM_H265 {
        headerSize = 2
    }

    hasVCL := false
    for start >= 0 {
        naluStart := start + int(startCodeSize)
        if naluStart+headerSize >= len(data) {
            break
        }
        nalu := data[naluStart:]
        var isVCL, startsAccessUnit bool
        if stream.cid == TS_STREAM_H264 {
            kind := codec.H264NaluTypeWithoutStartCode(nalu)
            isVCL = codec.IsH264VCLNaluType(kind)
            switch kind {
            case codec.H264_NAL_AUD, codec.H264_NAL_SPS, codec.H264_NAL_PPS, codec.H264_NAL_SEI:
                startsAccessUnit = true
            default:
                startsAccessUnit = isVCL && nalu[headerSize]&0x80 != 0
            }
        } else {
            kind := codec.H265NaluTypeWithoutStartCode(nalu)
            isVCL = codec.IsH265VCLNaluType(kind)
            switch kind {
            case codec.H265_NAL_AUD, codec.H265_NAL_VPS, codec.H265_NAL_SPS, codec.H265_NAL_PPS, codec.H265_NAL_SEI:
                startsAccessUnit = true
            default:
                startsAccessUnit = isVCL && nalu[headerSize]&0x80 != 0
            }
        }

        if hasVCL && startsAccessUnit {
            if demuxer.OnFrame != nil {
                frame := stripLeadingVideoAUD(stream.cid, data[frameStart:start])
                demuxer.OnFrame(stream.cid, frame, stream.pkg.pts/90, stream.pkg.dts/90)
            }
            frameStart = start
            stream.pkg.videoTimestamps.applyThrough(frameStart, &stream.pkg.pts, &stream.pkg.dts)
            hasVCL = false
        }
        hasVCL = hasVCL || isVCL
        start, startCodeSize = codec.FindStartCode(data, naluStart)
    }

    if frameStart > 0 {
        size := copy(data, data[frameStart:])
        stream.pkg.payload = data[:size]
        stream.pkg.videoTimestamps.removePrefix(frameStart)
    }
}
