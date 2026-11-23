package mpeg2

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestTSDemuxerReadsPESHeaderAcrossTransportPackets(t *testing.T) {
	muxer, wire := tsProgramTestMuxer()
	program := &table_pmt{pid: 0x200, pm: 1, pcr_pid: 0x100}
	stream := NewPESStream(0x100, TS_STREAM_AAC)
	writeTSProgramTestPAT(muxer, 0, 0, 0, PmtPair{1, program.pid})
	writeTSProgramTestPMT(muxer, program, stream)

	pes := NewPesPacket()
	pes.Stream_id = uint8(PES_STREAM_AUDIO)
	pes.PTS_DTS_flags = 3
	pes.PES_header_data_length = 10
	pes.Pts, pes.Dts = 9000, 9000
	pes.Pes_payload = []byte(clockTestAAC)
	pes.PES_packet_length = uint16(3 + int(pes.PES_header_data_length) + len(pes.Pes_payload))
	header := codec.NewBitStreamWriter(32)
	pes.Encode(header)
	data := header.Bits()
	writeTSTestPayload(muxer, stream.pid, 0, 1, data[:6])
	writeTSTestPayload(muxer, stream.pid, 1, 0, data[6:13])
	writeTSTestPayload(muxer, stream.pid, 2, 0, data[13:])

	_, samples := readTSProgramTestSamples(t, wire)
	checkTSProgramTestSamples(t, samples, []tsProgramTestSample{{TS_STREAM_AAC, clockTestAAC, 100}})
}

func TestTSDemuxerFindsLastCompletePacketAfterJunk(t *testing.T) {
	const pid = 0x400
	for _, prefixSize := range []int{1, 181, 377, 767} {
		t.Run(strconv.Itoa(prefixSize), func(t *testing.T) {
			muxer, wire := tsProgramTestMuxer()
			wire.Write(bytes.Repeat([]byte{0x99}, prefixSize))
			writeTSTestPayload(muxer, pid, 0, 0, []byte{0xff})

			demuxer := NewTSDemuxer()
			var packets int
			demuxer.OnTSPacket = func(packet *TSPacket) {
				if packet.PID != pid {
					t.Errorf("PID = %#x, want %#x", packet.PID, pid)
				}
				packets++
			}
			if err := demuxer.Input(wire); err != nil {
				t.Fatal(err)
			}
			if packets != 1 {
				t.Errorf("read %d packets, want 1", packets)
			}
		})
	}
}

func writeTSTestPayload(muxer *TSMuxer, pid uint16, cc, start uint8, payload []byte) {
	header := TSPacket{
		PID:                          pid,
		Payload_unit_start_indicator: start,
		Continuity_counter:           cc,
		Adaptation_field_control:     3,
		Field: &Adaptation_field{
			Stuffing_byte: uint8(TS_PAKCET_SIZE - 6 - len(payload)),
		},
	}
	packet := codec.NewBitStreamWriter(TS_PAKCET_SIZE)
	header.EncodeHeader(packet)
	packet.PutBytes(payload)
	muxer.OnPacket(packet.Bits())
}
