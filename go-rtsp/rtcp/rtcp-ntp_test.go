package rtcp

import (
	"testing"
	"time"
)

func TestNTPClockConversion(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stamp uint64
		utc   time.Time
	}{
		{name: "NTP epoch", utc: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)},
		{name: "half second before Unix epoch", stamp: 0x83aa7e7f80000000, utc: time.Unix(-1, 500000000).UTC()},
		{name: "half second in 2025", stamp: 0xeb21296580000000, utc: time.Date(2025, 1, 2, 15, 4, 5, 500000000, time.UTC)},
		{name: "one nanosecond", stamp: 0x83aa7e8000000004, utc: time.Unix(0, 1).UTC()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NTP2UtcClock(tc.stamp); got != tc.utc {
				t.Errorf("decoded time = %s, want %s", got, tc.utc)
			}
			if got := UtcClockToNTP(tc.utc); got != tc.stamp {
				t.Errorf("encoded time = %#016x, want %#016x", got, tc.stamp)
			}
		})
	}
}

func TestReceiverReportLastSenderTimestamp(t *testing.T) {
	ctx := NewRtcpContext(1, 41, 90000)
	if block := ctx.GenerateRR().Blocks[0]; block.Lsr != 0 {
		t.Errorf("LSR before any sender report = %#x, want 0", block.Lsr)
	}
	report := NewSenderReport()
	report.SSRC = 7
	report.NTP = 0xe552789abcde1234
	ctx.ReceivedSR(report)
	if got := ctx.GenerateRR().Blocks[0].Lsr; got != 0x789abcde {
		t.Errorf("LSR = %#08x, want 0x789abcde", got)
	}
}
