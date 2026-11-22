package rtcp

import (
	"bytes"
	"reflect"
	"testing"
)

func TestRTCPByeSourcesAndOptionalReason(t *testing.T) {
	for _, tc := range []struct {
		wire    []byte
		sources []uint32
		reason  string
	}{
		{wire: []byte{0x82, 203, 0, 4, 0, 0, 0, 6, 0, 0, 0, 7, 4, 'l', 'e', 'f', 't', 0, 0, 0}, sources: []uint32{6, 7}, reason: "left"},
		{wire: []byte{0x81, 203, 0, 1, 0, 0, 0, 6}, sources: []uint32{6}},
	} {
		bye := NewBye()
		bye.SSRCS, bye.Reason = tc.sources, tc.reason
		if got := bye.Encode(); !bytes.Equal(got, tc.wire) {
			t.Errorf("encoded BYE = %x, want %x", got, tc.wire)
		}
		decoded := NewBye()
		if err := decoded.Decode(tc.wire); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(decoded.SSRCS, tc.sources) || decoded.Reason != tc.reason || int(decoded.ReasonLen) != len(tc.reason) {
			t.Errorf("decoded sources/reason = %v/%q, want %v/%q", decoded.SSRCS, decoded.Reason, tc.sources, tc.reason)
		}
	}
}

func TestRTCPByePaddingStaysOutsideReason(t *testing.T) {
	wire := []byte{0xa1, 203, 0, 3, 0, 0, 0, 6, 2, 'o', 'k', 0, 0, 0, 0, 4}
	bye := NewBye()
	if err := bye.Decode(wire); err != nil {
		t.Fatal(err)
	}
	if bye.Reason != "ok" || !bytes.Equal(bye.PaddingData, []byte{0, 0, 0}) {
		t.Errorf("reason = %q, padding = %x, want ok and 3 padding bytes", bye.Reason, bye.PaddingData)
	}
	if got := bye.Encode(); !bytes.Equal(got, wire) {
		t.Errorf("re-encoded BYE = %x, want %x", got, wire)
	}
}
