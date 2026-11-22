package rtcp

import (
	"reflect"
	"testing"

	"github.com/yapingcat/gomedia/go-rtsp/rtp"
)

func TestReportBlockDecodesAllLossBits(t *testing.T) {
	for _, count := range []uint32{0, 0x012345, 0x7fffff, 0x800000, 0xffffff} {
		want := ReportBlock{SSRC: 7, Fraction: 42, Lost: count, ExtendHighestSeq: 12345}
		var got ReportBlock
		if err := got.Decode(want.Encode()); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("decoded loss bits = %#06x, want %#06x", got.Lost, want.Lost)
		}
	}
}

func TestReceiverReportJitterWithReorderedTimestamps(t *testing.T) {
	for _, base := range []uint32{0, 0xffffe000} {
		ctx := NewRtcpContext(1, 41, 90000)
		for i, value := range []uint32{0, 9000, 6000, 12000} {
			ctx.ReceivedRtp(&rtp.RtpPacket{Header: rtp.RtpHdr{SequenceNumber: uint16(41 + i), Timestamp: base + value}})
		}
		if got := ctx.GenerateRR().Blocks[0].Jitter; got > 90000 {
			t.Errorf("RTP clocks from %#x: jitter = %d samples, want less than one second", base, got)
		}
	}
}

func TestReceiverReportDoesNotCountDuplicatesAsLoss(t *testing.T) {
	ctx := NewRtcpContext(1, 41, 90000)
	receive := func(seq uint16) {
		ctx.ReceivedRtp(&rtp.RtpPacket{Header: rtp.RtpHdr{SequenceNumber: seq, Timestamp: uint32(seq) * 3000}})
	}
	receive(41)
	receive(42)
	ctx.GenerateRR()

	receive(43)
	receive(44)
	receive(44)
	if got := ctx.GenerateRR().Blocks[0].Fraction; got != 0 {
		t.Errorf("fraction reported as lost with a duplicate = %d/256, want 0", got)
	}

	// The following interval has a genuine gap at sequence number 45.
	receive(46)
	if got := ctx.GenerateRR().Blocks[0].Fraction; got != 128 {
		t.Errorf("fraction reported as lost with one of two missing = %d/256, want 128/256", got)
	}
}

func TestSenderReportDecodesEachReportBlock(t *testing.T) {
	sent := NewSenderReport()
	sent.RC = 2
	sent.SSRC = 5
	sent.Blocks = []ReportBlock{{SSRC: 6, Jitter: 10}, {SSRC: 7, Jitter: 20}}
	got := NewSenderReport()
	if err := got.Decode(sent.Encode()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Blocks, sent.Blocks) {
		t.Errorf("decoded blocks = %+v, want %+v", got.Blocks, sent.Blocks)
	}

	sent.RC = 1
	sent.Blocks = sent.Blocks[1:]
	if err := got.Decode(sent.Encode()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Blocks, sent.Blocks) {
		t.Errorf("decoded blocks after reuse = %+v, want %+v", got.Blocks, sent.Blocks)
	}
}
