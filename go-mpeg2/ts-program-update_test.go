package mpeg2

import (
	"bytes"
	"reflect"
	"testing"
)

type tsProgramTestSample struct {
	cid  TS_STREAM_TYPE
	data string
	pts  uint64
}

func TestTSDemuxerReplacesProgramsAfterCompletePAT(t *testing.T) {
	muxer, wire := tsProgramTestMuxer()
	first := &table_pmt{pid: 0x200, pm: 1, pcr_pid: 0x100}
	second := &table_pmt{pid: 0x201, pm: 2, pcr_pid: 0x100}
	stream := NewPESStream(0x100, TS_STREAM_AAC)
	writeTSProgramTestPAT(muxer, 0, 0, 0, PmtPair{1, first.pid})
	writeTSProgramTestPMT(muxer, first, stream)
	muxer.writePES(stream, first, []byte(clockTestAAC), 9000, 9000, false, false)

	// A new PAT arrives in two sections, in reverse order. Its partial
	// arrival must leave the active program readable until the last section.
	writeTSProgramTestPAT(muxer, 1, 1, 1, PmtPair{3, 0x202})
	muxer.writePES(stream, first, []byte(clockTestAAC), 10800, 10800, false, false)
	writeTSProgramTestPAT(muxer, 1, 0, 1, PmtPair{2, second.pid})

	stream.streamtype = TS_STREAM_AUDIO_MPEG1
	writeTSProgramTestPMT(muxer, second, stream)
	audio := append([]byte{0xff, 0xfd, 0x84, 0xc4}, make([]byte, 380)...)
	muxer.writePES(stream, second, audio, 12600, 12600, false, false)

	demuxer, samples := readTSProgramTestSamples(t, wire)
	want := []tsProgramTestSample{
		{TS_STREAM_AAC, clockTestAAC, 100},
		{TS_STREAM_AAC, clockTestAAC, 120},
		{TS_STREAM_AUDIO_MPEG1, string(audio), 140},
	}
	checkTSProgramTestSamples(t, samples, want)
	if len(demuxer.programs) != 2 || demuxer.programs[second.pid] == nil || demuxer.programs[0x202] == nil {
		t.Errorf("active programs = %v, want program 2 and program 3", demuxer.programs)
	}
}

func TestTSDemuxerDropsTracksMissingFromCurrentPMT(t *testing.T) {
	muxer, wire := tsProgramTestMuxer()
	first := &table_pmt{pid: 0x200, pm: 1, pcr_pid: 0x100}
	second := &table_pmt{pid: 0x201, pm: 2, pcr_pid: 0x100}
	stream := NewPESStream(0x100, TS_STREAM_AAC)
	writeTSProgramTestPAT(muxer, 0, 0, 0, PmtPair{1, first.pid}, PmtPair{2, second.pid})
	writeTSProgramTestPMT(muxer, first, stream)
	muxer.writePES(stream, first, []byte(clockTestAAC), 9000, 9000, false, false)

	replacement := NewPESStream(0x101, TS_STREAM_AAC)
	first.version_number++
	first.pcr_pid = replacement.pid
	writeTSProgramTestPMT(muxer, first, replacement)

	stream.streamtype = TS_STREAM_AUDIO_MPEG1
	writeTSProgramTestPMT(muxer, second, stream)
	audio := append([]byte{0xff, 0xfd, 0x84, 0xc4}, make([]byte, 380)...)
	muxer.writePES(stream, second, audio, 10800, 10800, false, false)

	demuxer, samples := readTSProgramTestSamples(t, wire)
	checkTSProgramTestSamples(t, samples, []tsProgramTestSample{
		{TS_STREAM_AAC, clockTestAAC, 100},
		{TS_STREAM_AUDIO_MPEG1, string(audio), 120},
	})
	if streams := demuxer.programs[first.pid].streams; len(streams) != 1 || streams[replacement.pid] == nil {
		t.Errorf("program 1 tracks = %v, want only PID %#x", streams, replacement.pid)
	}
}

func TestTSDemuxerKeepsProgramAfterDamagedPAT(t *testing.T) {
	muxer, wire := tsProgramTestMuxer()
	program := &table_pmt{pid: 0x200, pm: 1, pcr_pid: 0x100}
	stream := NewPESStream(0x100, TS_STREAM_AAC)
	writeTSProgramTestPAT(muxer, 0, 0, 0, PmtPair{1, program.pid})
	writeTSProgramTestPMT(muxer, program, stream)
	muxer.writePES(stream, program, []byte(clockTestAAC), 9000, 9000, false, false)

	writeTSSectionWithBadCRC(muxer, func() {
		writeTSProgramTestPAT(muxer, 1, 0, 0, PmtPair{2, 0x201})
	})
	muxer.writePES(stream, program, []byte(clockTestAAC), 10800, 10800, false, false)

	demuxer, samples := readTSProgramTestSamples(t, wire)
	checkTSProgramTestSamples(t, samples, []tsProgramTestSample{
		{TS_STREAM_AAC, clockTestAAC, 100},
		{TS_STREAM_AAC, clockTestAAC, 120},
	})
	if demuxer.programs[program.pid] == nil || demuxer.programs[0x201] != nil {
		t.Error("the damaged PAT replaced the active program")
	}
}

func TestTSDemuxerKeepsCodecAfterDamagedPMT(t *testing.T) {
	muxer, wire := tsProgramTestMuxer()
	program := &table_pmt{pid: 0x200, pm: 1, pcr_pid: 0x100}
	stream := NewPESStream(0x100, TS_STREAM_AAC)
	writeTSProgramTestPAT(muxer, 0, 0, 0, PmtPair{1, program.pid})
	writeTSProgramTestPMT(muxer, program, stream)
	muxer.writePES(stream, program, []byte(clockTestAAC), 9000, 9000, false, false)

	program.version_number++
	alternate := NewPESStream(stream.pid, TS_STREAM_AUDIO_MPEG1)
	writeTSSectionWithBadCRC(muxer, func() {
		writeTSProgramTestPMT(muxer, program, alternate)
	})
	muxer.writePES(stream, program, []byte(clockTestAAC), 10800, 10800, false, false)

	demuxer, samples := readTSProgramTestSamples(t, wire)
	checkTSProgramTestSamples(t, samples, []tsProgramTestSample{
		{TS_STREAM_AAC, clockTestAAC, 100},
		{TS_STREAM_AAC, clockTestAAC, 120},
	})
	if got := demuxer.programs[program.pid].streams[stream.pid].cid; got != TS_STREAM_AAC {
		t.Errorf("codec = %d, want AAC after the damaged PMT", got)
	}
}

func writeTSSectionWithBadCRC(muxer *TSMuxer, writeSection func()) {
	output := muxer.OnPacket
	muxer.OnPacket = func(packet []byte) {
		// The section fits in one packet and starts after the zero pointer.
		wire := append([]byte(nil), packet...)
		sectionLength := int(wire[6]&0x0f)<<8 | int(wire[7])
		wire[8+sectionLength-1] ^= 1
		output(wire)
	}
	writeSection()
	muxer.OnPacket = output
}

func tsProgramTestMuxer() (*TSMuxer, *bytes.Buffer) {
	var wire bytes.Buffer
	muxer := NewTSMuxer()
	muxer.OnPacket = func(packet []byte) { wire.Write(packet) }
	return muxer, &wire
}

func writeTSProgramTestPAT(muxer *TSMuxer, version, section, last uint8, programs ...PmtPair) {
	pat := NewPat()
	pat.Transport_stream_id = 1
	pat.Version_number = version
	pat.Section_number = section
	pat.Last_section_number = last
	pat.Pmts = programs
	muxer.writePat(pat)
}

func writeTSProgramTestPMT(muxer *TSMuxer, program *table_pmt, stream *pes_stream) {
	pmt := NewPmt()
	pmt.Program_number = program.pm
	pmt.Version_number = program.version_number
	pmt.PCR_PID = program.pcr_pid
	pmt.Streams = []StreamPair{{StreamType: uint8(stream.streamtype), Elementary_PID: stream.pid}}
	muxer.writePmt(pmt, program)
}

func readTSProgramTestSamples(t *testing.T, wire *bytes.Buffer) (*TSDemuxer, []tsProgramTestSample) {
	t.Helper()
	demuxer := NewTSDemuxer()
	var samples []tsProgramTestSample
	demuxer.OnFrame = func(cid TS_STREAM_TYPE, data []byte, pts, dts uint64) {
		if pts != dts {
			t.Errorf("PTS %d != DTS %d", pts, dts)
		}
		samples = append(samples, tsProgramTestSample{cid, string(data), pts})
	}
	if err := demuxer.Input(wire); err != nil {
		t.Fatal(err)
	}
	return demuxer, samples
}

func checkTSProgramTestSamples(t *testing.T, got, want []tsProgramTestSample) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		var timings [][2]uint64
		for _, sample := range got {
			timings = append(timings, [2]uint64{uint64(sample.cid), sample.pts})
		}
		t.Errorf("received %d samples (codec, PTS)=%v, want %d matching samples", len(got), timings, len(want))
	}
}
