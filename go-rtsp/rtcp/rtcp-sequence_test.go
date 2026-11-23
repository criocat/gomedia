package rtcp

import (
	"testing"

	"github.com/yapingcat/gomedia/go-rtsp/rtp"
)

func TestRTCPSequenceRestartRequiresConsecutivePackets(t *testing.T) {
	const sender = 0x01020304
	ctx := NewRtcpContext(1, 9999, 90000)
	receive := func(sequence uint16) {
		ctx.ReceivedRtp(&rtp.RtpPacket{Header: rtp.RtpHdr{
			SSRC: sender, SequenceNumber: sequence, Timestamp: uint32(sequence) * 3000,
		}})
	}
	checkHighest := func(want uint32) {
		t.Helper()
		block := ctx.GenerateRR().Blocks[0]
		if block.ExtendHighestSeq != want || block.SSRC != sender || block.Lost != 0 {
			t.Errorf("report sequence=%d SSRC=%#x lost=%d, want sequence=%d SSRC=%#x and zero loss",
				block.ExtendHighestSeq, block.SSRC, block.Lost, want, sender)
		}
	}

	receive(9999)
	receive(10000)
	checkHighest(10000)
	receive(1)
	checkHighest(10000)
	receive(10001)
	checkHighest(10001)

	receive(0)
	checkHighest(10001)
	receive(1)
	checkHighest(1)
}

func TestRTCPDoesNotReportLossBeforeRTPValidation(t *testing.T) {
	const sender = 0x01020304
	ctx := NewRtcpContext(1, 10000, 90000)
	ctx.ReceivedSR(&SenderReport{SSRC: sender, NTP: 0x0123456789abcdef})
	check := func(sequence uint32) {
		t.Helper()
		block := ctx.GenerateRR().Blocks[0]
		if block.SSRC != sender || block.Lsr != 0x456789ab {
			t.Errorf("report SSRC=%#x LSR=%#x, want %#x and %#x", block.SSRC, block.Lsr, sender, 0x456789ab)
		}
		if block.ExtendHighestSeq != sequence || block.Lost != 0 || block.Fraction != 0 {
			t.Errorf("report sequence=%d lost=%d fraction=%d, want sequence=%d and zero loss",
				block.ExtendHighestSeq, block.Lost, block.Fraction, sequence)
		}
	}
	check(0)
	ctx.ReceivedRtp(&rtp.RtpPacket{Header: rtp.RtpHdr{SSRC: sender, SequenceNumber: 10000}})
	check(0)
	ctx.ReceivedRtp(&rtp.RtpPacket{Header: rtp.RtpHdr{SSRC: sender, SequenceNumber: 10001}})
	check(10001)
}

func TestRTCPResetsJitterAfterRTPRestart(t *testing.T) {
	ctx := NewRtcpContext(1, 9999, 90000)
	receive := func(sequence uint16, timestamp uint32) {
		ctx.ReceivedRtp(&rtp.RtpPacket{Header: rtp.RtpHdr{
			SSRC: 7, SequenceNumber: sequence, Timestamp: timestamp,
		}})
	}
	receive(9999, 9000)
	receive(10000, 12000)
	receive(0, 0x40000000)
	receive(1, 0x40000000+3000)

	block := ctx.GenerateRR().Blocks[0]
	if block.ExtendHighestSeq != 1 || block.Jitter != 0 {
		t.Errorf("sequence=%d jitter=%d after restart, want sequence=1 and zero jitter", block.ExtendHighestSeq, block.Jitter)
	}
}
