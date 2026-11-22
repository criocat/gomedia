package rtsp

import (
	"strings"
	"testing"
)

func TestRTSPTransportKeepsBothUDPPortPairs(t *testing.T) {
	transport := NewRtspTransport(WithEnableUdp())
	transport.SetClientUdpPort(5000, 5001)
	transport.SetServerUdpPort(6000, 6001)
	wire := transport.EncodeString()
	if !strings.Contains(wire, ";client_port=5000-5001;") || !strings.Contains(wire, ";server_port=6000-6001;") {
		t.Errorf("Transport = %q, want separate client and server ports", wire)
	}
	got := NewRtspTransport()
	if err := got.DecodeString(wire); err != nil {
		t.Fatal(err)
	}
	if got.Proto != UDP || got.Client_ports != [2]uint16{5000, 5001} || got.Server_ports != [2]uint16{6000, 6001} {
		t.Errorf("decoded Transport = %+v, want UDP and both port pairs", got)
	}
}

func TestRTSPRtpInfoRoundTrip(t *testing.T) {
	const streamURL = "rtsp://example.test/lecture/streamid=0?key=value"
	info := NewRtpInfo(streamURL, 42)
	var decoded RtpInfo
	for _, tc := range []struct {
		value int64
		field string
	}{
		{value: 4294967295, field: ";rtptime=4294967295"},
		{value: 0, field: ";rtptime=0"},
		{value: -1},
	} {
		info.Rtptime = tc.value
		want := "url=" + streamURL + ";seq=42" + tc.field
		if got := info.EncodeString(); got != want {
			t.Errorf("RTP-Info = %q, want %q", got, want)
		}
		decoded.Decode(want)
		if decoded != *info {
			t.Errorf("decoded RTP-Info = %+v, want %+v", decoded, *info)
		}
	}
	if got := NewRtpInfo(streamURL, 42).EncodeString(); got != "url="+streamURL+";seq=42" {
		t.Errorf("default RTP-Info = %q, want an absent rtptime", got)
	}
}
