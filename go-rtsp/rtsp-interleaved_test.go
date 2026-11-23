package rtsp

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/yapingcat/gomedia/go-rtsp/rtcp"
	"github.com/yapingcat/gomedia/go-rtsp/rtp"
)

func TestRTSPIgnoresInterleavedFramesForUnconfiguredTracks(t *testing.T) {
	for _, peer := range []string{"server", "client"} {
		t.Run(peer, func(t *testing.T) {
			audio := NewAudioTrack(NewAudioCodec("PCMA", 8, 8000, 1))
			audio.SetTransport(NewRtspTransport(WithTcpInterleaved([2]int{0, 1})))
			video := NewVideoTrack(NewVideoCodec("H264", 96, 90000))
			endpoint := newInterleavedTestEndpoint(t, peer, audio, video)

			var samples [][]byte
			audio.OnSample(func(sample RtspSample) {
				samples = append(samples, append([]byte(nil), sample.Sample...))
			})
			packet := rtp.RtpPacket{
				Header:  rtp.RtpHdr{PayloadType: 8, SequenceNumber: 1, SSRC: 77},
				Payload: []byte{0x55, 0xaa},
			}
			if err := endpoint.Input(interleavedTestFrame(2, packet.Encode())); err != nil {
				t.Fatal(err)
			}
			if len(samples) != 0 {
				t.Fatal("an unassigned channel delivered a sample")
			}
			if err := endpoint.Input(interleavedTestFrame(0, packet.Encode())); err != nil {
				t.Fatal(err)
			}
			if len(samples) != 1 || !bytes.Equal(samples[0], packet.Payload) {
				t.Errorf("samples=%x, want one packet from channel 0", samples)
			}
		})
	}
}

func TestRTSPIgnoresInterleavedFramesForUDPTracks(t *testing.T) {
	for _, peer := range []string{"server", "client"} {
		t.Run(peer, func(t *testing.T) {
			const source = 77
			audio := NewAudioTrack(NewAudioCodec("PCMA", 8, 8000, 1), WithDisableRtcpRR())
			audio.SetTransport(NewRtspTransport(WithEnableUdp(), WithClientUdpPort(5000, 5001)))
			endpoint := newInterleavedTestEndpoint(t, peer, audio)
			for _, seq := range []uint16{41, 42} {
				packet := rtp.RtpPacket{
					Header:  rtp.RtpHdr{PayloadType: 8, SequenceNumber: seq, SSRC: source},
					Payload: []byte{0x55},
				}
				if err := audio.Input(packet.Encode(), false); err != nil {
					t.Fatal(err)
				}
			}

			report := rtcp.NewSenderReport()
			report.SSRC = source
			report.NTP = 0x0123456789abcdef
			if err := endpoint.Input(interleavedTestFrame(0, report.Encode())); err != nil {
				t.Fatal(err)
			}
			if lsr := audio.GetRtcpRecvContext().GenerateRR().Blocks[0].Lsr; lsr != 0 {
				t.Errorf("UDP track received interleaved RTCP, LSR=%#x", lsr)
			}
		})
	}
}

type interleavedTestEndpoint interface {
	Input([]byte) error
}

func newInterleavedTestEndpoint(t *testing.T, peer string, tracks ...*RtspTrack) interleavedTestEndpoint {
	t.Helper()
	selected := make(map[string]*RtspTrack)
	for _, track := range tracks {
		selected[track.TrackName] = track
	}
	if peer == "server" {
		server := NewRtspServer(nil)
		server.tracks = selected
		return server
	}
	client, err := NewRtspClient("rtsp://localhost/live", nil)
	if err != nil {
		t.Fatal(err)
	}
	client.tracks = selected
	return client
}

func interleavedTestFrame(channel byte, payload []byte) []byte {
	header := []byte{'$', channel, 0, 0}
	binary.BigEndian.PutUint16(header[2:], uint16(len(payload)))
	return append(header, payload...)
}
