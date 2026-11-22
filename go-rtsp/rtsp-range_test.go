package rtsp

import (
	"testing"
	"time"
)

func TestRTSPNPTRangeRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		wire, encoded string
		begin, end    int64
	}{
		{wire: "npt=now-", encoded: "npt=now-", begin: -1, end: -1},
		{wire: "npt=12:05:35.3-", encoded: "npt=43535.300-", begin: 43535300, end: -1},
		{wire: "npt=0.003-0.005", encoded: "npt=0.003-0.005", begin: 3, end: 5},
		{wire: "npt=-42.001", encoded: "npt=-42.001", begin: -2, end: 42001},
		{wire: "npt=1000000.0019-", encoded: "npt=1000000.001-", begin: 1000000001, end: -1},
	} {
		t.Run(tc.wire, func(t *testing.T) {
			rt, err := parseRange(tc.wire)
			if err != nil {
				t.Fatal(err)
			}
			if rt.begin != tc.begin || rt.end != tc.end {
				t.Errorf("parsed range = [%d, %d], want [%d, %d] ms", rt.begin, rt.end, tc.begin, tc.end)
			}
			if got := rt.EncodeString(); got != tc.encoded {
				t.Errorf("encoded range = %q, want %q", got, tc.encoded)
			}
		})
	}
}

func TestRTSPUTCRangeRoundTrip(t *testing.T) {
	local := time.Local
	time.Local = time.FixedZone("UTC+03", 3*60*60)
	t.Cleanup(func() { time.Local = local })
	begin := time.Date(2025, 1, 2, 15, 4, 5, 125000000, time.UTC).Unix()*1000 + 125
	for _, tc := range []struct {
		wire string
		end  int64
	}{
		{wire: "clock=20250102T150405.125Z-20250102T150505Z", end: begin + 60000 - 125},
		{wire: "clock=20250102T150405.125Z-", end: -1},
	} {
		rt, err := parseRange(tc.wire)
		if err != nil {
			t.Fatal(err)
		}
		if rt.begin != begin || rt.end != tc.end {
			t.Errorf("parsed %q as [%d, %d], want [%d, %d] ms", tc.wire, rt.begin, rt.end, begin, tc.end)
		}
		if got := rt.EncodeString(); got != tc.wire {
			t.Errorf("encoded UTC range = %q, want %q", got, tc.wire)
		}
	}
}
