package sdp

import (
	"reflect"
	"strings"
	"testing"
)

func TestSDPSessionConnectionRoundTrip(t *testing.T) {
	for _, connection := range []string{"IN IP4 192.0.2.1", "IN IP6 ff15::101/2"} {
		input := "v=0\r\no=- 0 0 IN IP4 192.0.2.1\r\ns=Example\r\nc=" + connection + "\r\nt=0 0\r\nm=video 5000 RTP/AVP 96\r\n"
		var parsed, roundTrip Sdp
		if err := parsed.ParserSdp(input); err != nil {
			t.Fatal(err)
		}
		encoded := parsed.Encode()
		if !strings.Contains(encoded, "\r\nc="+connection+"\r\n") {
			t.Errorf("encoded SDP = %q, want connection %q", encoded, connection)
		}
		if err := roundTrip.ParserSdp(encoded); err != nil {
			t.Fatal(err)
		}
		if roundTrip.ConnectionData != parsed.ConnectionData {
			t.Errorf("connection = %+v, want %+v", roundTrip.ConnectionData, parsed.ConnectionData)
		}
	}
}

func TestSDPMediaPortRange(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  []uint16
	}{
		{input: "video 5000/3 RTP/AVP 96", want: []uint16{5000, 5002, 5004}},
		{input: "video 5000/3 UDP/TLS/RTP/SAVPF 96", want: []uint16{5000, 5002, 5004}},
		{input: "video 5000/3 udp 96", want: []uint16{5000, 5001, 5002}},
		{input: "video 5000 RTP/AVP 96", want: []uint16{5000}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			var media Media
			if err := media.ParseMLine(tc.input); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(media.Ports, tc.want) {
				t.Errorf("ports = %v, want %v", media.Ports, tc.want)
			}
			if got := media.Encode(); got != "m="+tc.input+"\r\n" {
				t.Errorf("encoded media line = %q, want %q", got, tc.input)
			}
		})
	}
}
