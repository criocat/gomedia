package sdp

import "testing"

func TestSDPUsesAttributesForPreferredPayload(t *testing.T) {
	var parsed Sdp
	input := "m=audio 5000 RTP/AVP 96 97\r\n" +
		"a=rtpmap:97 opus/48000/2\r\n" +
		"a=fmtp:96 config=1408\r\n" +
		"a=rtpmap:96 MPEG4-GENERIC/16000/1\r\n" +
		"a=fmtp:97 stereo=1\r\n" +
		"a=rtpmap:97 opus/48000/2\r\n" +
		"m=audio 5002 RTP/AVP 0 98\r\n" +
		"a=rtpmap:98 opus/48000/2\r\n"
	if err := parsed.ParserSdp(input); err != nil {
		t.Fatal(err)
	}
	dynamic := parsed.Medias[0]
	if dynamic.PayloadType != 96 || dynamic.EncodeName != "MPEG4-GENERIC" || dynamic.ClockRate != 16000 || dynamic.ChannelCount != 1 {
		t.Errorf("wrong dynamic payload configuration: %+v", dynamic)
	}
	if dynamic.Attrs["rtpmap"] != "96 MPEG4-GENERIC/16000/1" || dynamic.Attrs["fmtp"] != "96 config=1408" {
		t.Errorf("wrong dynamic payload attributes: %v", dynamic.Attrs)
	}
	static := parsed.Medias[1]
	if static.PayloadType != 0 || static.EncodeName != "PCMU" || static.ClockRate != 8000 || static.ChannelCount != 1 {
		t.Errorf("wrong static payload configuration: %+v", static)
	}
}

func TestSDPRTPMapAudioDefaultsToMono(t *testing.T) {
	var parsed Sdp
	input := "m=audio 5000 RTP/AVP 97\r\na=rtpmap:97 PCMA/8000\r\n" +
		"m=audio 5002 RTP/AVP 98\r\na=rtpmap:98 MPEG4-GENERIC/48000/2\r\n"
	if err := parsed.ParserSdp(input); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Medias) != 2 {
		t.Fatalf("parsed %d media descriptions, want 2", len(parsed.Medias))
	}
	if mono, stereo := parsed.Medias[0].ChannelCount, parsed.Medias[1].ChannelCount; mono != 1 || stereo != 2 {
		t.Errorf("audio channel counts = %d, %d; want 1, 2", mono, stereo)
	}
}
