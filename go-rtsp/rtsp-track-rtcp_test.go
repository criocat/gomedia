package rtsp

import (
	"testing"

	"github.com/yapingcat/gomedia/go-rtsp/rtcp"
	"github.com/yapingcat/gomedia/go-rtsp/rtp"
)

const rtcpTestAudioSSRC = 77
const rtcpTestSenderNTP = 0x0123456789abcdef

func TestRTSPReceivesSenderReportInCompoundRTCP(t *testing.T) {
	track := newTrackReceivingRtcpTestAudio(t, WithDisableRtcpRR())
	receiver := rtcp.NewReceiverReport()
	receiver.SSRC = 88
	sender := rtcp.NewSenderReport()
	sender.SSRC = rtcpTestAudioSSRC
	sender.NTP = rtcpTestSenderNTP
	sdes := rtcp.NewRtcpContext(sender.SSRC, 41, 8000).GenerateSDES(rtcp.SDES_CNAME, "audio@example.test")

	compound := append(receiver.Encode(), sender.Encode()...)
	compound = append(compound, sdes.Encode()...)
	if err := track.Input(compound, true); err != nil {
		t.Fatal(err)
	}
	checkTrackLSR(t, track)
}

func TestRTSPReceivesSenderReportWithoutOutput(t *testing.T) {
	track := newTrackReceivingRtcpTestAudio(t)
	sender := rtcp.NewSenderReport()
	sender.SSRC = rtcpTestAudioSSRC
	sender.NTP = rtcpTestSenderNTP
	if err := track.Input(sender.Encode(), true); err != nil {
		t.Fatal(err)
	}
	checkTrackLSR(t, track)

	var reports int
	track.OnPacket(func(data []byte, isRTCP bool) error {
		var report rtcp.ReceiverReport
		if err := report.Decode(data); err != nil {
			return err
		}
		if !isRTCP || len(report.Blocks) != 1 || report.Blocks[0].Lsr != 0x456789ab {
			t.Errorf("unexpected receiver report %+v", report)
		}
		reports++
		return nil
	})
	if err := track.Input(sender.Encode(), true); err != nil {
		t.Fatal(err)
	}
	if reports != 1 {
		t.Errorf("sent %d reports with an output callback, want 1", reports)
	}
}

func newTrackReceivingRtcpTestAudio(t *testing.T, options ...TrackOption) *RtspTrack {
	t.Helper()
	track := NewAudioTrack(NewAudioCodec("PCMA", 8, 8000, 1), options...)
	for _, seq := range []uint16{41, 42} {
		packet := rtp.RtpPacket{
			Header:  rtp.RtpHdr{PayloadType: 8, SequenceNumber: seq, SSRC: rtcpTestAudioSSRC},
			Payload: []byte{0x55},
		}
		if err := track.Input(packet.Encode(), false); err != nil {
			t.Fatal(err)
		}
	}
	return track
}

func checkTrackLSR(t *testing.T, track *RtspTrack) {
	t.Helper()
	if got := track.GetRtcpRecvContext().GenerateRR().Blocks[0].Lsr; got != 0x456789ab {
		t.Errorf("LSR=%#x, want %#x", got, 0x456789ab)
	}
}
