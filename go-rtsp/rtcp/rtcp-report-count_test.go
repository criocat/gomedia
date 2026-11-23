package rtcp

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"
)

func TestReceiverReportReplacesDecodedBlocks(t *testing.T) {
	report := NewReceiverReport()
	for _, blocks := range [][]ReportBlock{
		{{SSRC: 101, Jitter: 1}, {SSRC: 102, Jitter: 2}},
		{{SSRC: 103, Jitter: 3}},
		{},
	} {
		source := NewReceiverReport()
		source.RC = uint8(len(blocks))
		source.Blocks = blocks
		if err := report.Decode(source.Encode()); err != nil {
			t.Fatal(err)
		}
		if len(report.Blocks) != len(blocks) || len(blocks) > 0 && !reflect.DeepEqual(report.Blocks, blocks) {
			t.Errorf("decoded blocks = %+v, want %+v", report.Blocks, blocks)
		}
	}
}

func TestReportsEncodeTheirBlockCount(t *testing.T) {
	for _, count := range []int{0, 2, 31, 32} {
		t.Run(fmt.Sprintf("%d-blocks", count), func(t *testing.T) {
			blocks := make([]ReportBlock, count)
			for i := range blocks {
				blocks[i] = ReportBlock{SSRC: uint32(i + 101)}
			}
			want := count
			if want > 31 {
				want = 31
			}
			check := func(wire []byte, headerSize int, decodedCount uint8, decodedBlocks []ReportBlock) {
				t.Helper()
				if int(wire[0]&0x1f) != want || int(decodedCount) != want || len(decodedBlocks) != want {
					t.Errorf("header/decoded counts = %d/%d/%d, want %d",
						wire[0]&0x1f, decodedCount, len(decodedBlocks), want)
				}
				if length := (int(binary.BigEndian.Uint16(wire[2:])) + 1) * 4; length != headerSize+24*want || length != len(wire) {
					t.Errorf("header/actual length = %d/%d, want %d", length, len(wire), headerSize+24*want)
				}
				if want > 0 && !reflect.DeepEqual(decodedBlocks, blocks[:want]) {
					t.Errorf("decoded blocks = %+v, want %+v", decodedBlocks, blocks[:want])
				}
			}

			rr := NewReceiverReport()
			rr.Blocks = blocks
			rrWire := rr.Encode()
			decodedRR := NewReceiverReport()
			if err := decodedRR.Decode(rrWire); err != nil {
				t.Fatal(err)
			}
			check(rrWire, 8, decodedRR.RC, decodedRR.Blocks)

			sr := NewSenderReport()
			sr.Blocks = blocks
			srWire := sr.Encode()
			decodedSR := NewSenderReport()
			if err := decodedSR.Decode(srWire); err != nil {
				t.Fatal(err)
			}
			check(srWire, 28, decodedSR.RC, decodedSR.Blocks)
		})
	}
}
