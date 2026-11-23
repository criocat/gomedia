package rtsp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yapingcat/gomedia/go-rtsp/sdp"
)

type optionsTestHandler struct{ ServerHandle }

func (optionsTestHandler) HandleOption(*RtspServer, RtspRequest, *RtspResponse) {}

func TestOptionsEchoesCSeqRegardlessOfCase(t *testing.T) {
	for _, name := range []string{"CSeq", "cseq", "CSEQ"} {
		t.Run(name, func(t *testing.T) {
			server := NewRtspServer(optionsTestHandler{})
			var response string
			server.SetOutput(func(data []byte) error {
				response = string(data)
				return nil
			})
			request := fmt.Sprintf("OPTIONS rtsp://localhost/live RTSP/1.0\r\n%s: 17\r\n\r\n", name)
			if err := server.Input([]byte(request)); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(response, "\r\nCSeq: 17\r\n") {
				t.Errorf("response did not echo CSeq 17: %q", response)
			}
		})
	}
}

type controlTestClient struct {
	ClientHandle
	request RtspRequest
}

func (*controlTestClient) HandleOption(*RtspClient, RtspResponse, []string) error {
	return nil
}

func (*controlTestClient) HandleDescribe(*RtspClient, RtspResponse, *sdp.Sdp, map[string]*RtspTrack) error {
	return nil
}

func (*controlTestClient) HandleSetup(*RtspClient, RtspResponse, *RtspTrack, map[string]*RtspTrack, string, int) error {
	return nil
}

func (*controlTestClient) HandlePlay(*RtspClient, RtspResponse, *RangeTime, *RtpInfo) error {
	return nil
}

func (h *controlTestClient) HandleRequest(_ *RtspClient, request RtspRequest) error {
	h.request = request
	return nil
}

func startControlTestClient(t *testing.T, handler ClientHandle, description string) (*RtspClient, *[]string) {
	t.Helper()
	client, err := NewRtspClient("rtsp://localhost/source", handler)
	if err != nil {
		t.Fatal(err)
	}
	var sent []string
	client.SetOutput(func(data []byte) error {
		sent = append(sent, string(data))
		return nil
	})
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	options := "RTSP/1.0 200 OK\r\nCSeq: 1\r\nPublic: OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN, GET_PARAMETER\r\n\r\n"
	feedControlMessage(t, client, options)
	describe := fmt.Sprintf("RTSP/1.0 200 OK\r\nCSeq: 2\r\nContent-Type: application/sdp\r\n"+
		"Content-Base: rtsp://localhost/source/\r\nContent-Length: %d\r\n\r\n%s", len(description), description)
	feedControlMessage(t, client, describe)
	if len(sent) != 3 || !strings.HasPrefix(sent[2], "SETUP ") {
		t.Fatalf("expected OPTIONS, DESCRIBE and SETUP, got %q", sent)
	}
	return client, &sent
}

const controlTestSDP = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=example\r\n" +
	"c=IN IP4 127.0.0.1\r\nt=0 0\r\na=control:*\r\n" +
	"m=audio 0 RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n"

func feedControlMessage(t *testing.T, client *RtspClient, message string) {
	t.Helper()
	if err := client.Input([]byte(message)); err != nil {
		t.Fatal(err)
	}
}

func TestClientDeliversServerKeepAlive(t *testing.T) {
	handler := &controlTestClient{}
	client, sent := startControlTestClient(t, handler, controlTestSDP+"a=control:audio\r\n")
	feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 3\r\nSession: 12345678\r\n"+
		"Transport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n\r\n")
	if len(*sent) != 4 || !strings.HasPrefix((*sent)[3], "PLAY ") {
		t.Fatalf("expected PLAY after successful SETUP, got %q", *sent)
	}
	feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 4\r\nSession: 12345678\r\n\r\n")
	feedControlMessage(t, client, "GET_PARAMETER rtsp://localhost/source RTSP/1.0\r\ncseq: 33\r\nSession: 12345678\r\n\r\n")
	if got := handler.request; got.Method != GET_PARAMETER || got.Fileds[CSeq] != "33" || got.Fileds[Session] != "12345678" {
		t.Errorf("unexpected callback request: %+v", got)
	}
}

func TestClientResolvesSDPControlPath(t *testing.T) {
	tests := []struct {
		name    string
		control string
		uri     string
	}{
		{"relative", "audio", "rtsp://localhost/source/audio"},
		{"absolute_path", "/alternate/audio", "rtsp://localhost/alternate/audio"},
		{"absolute_uri", "rtsp://localhost/alternate/audio", "rtsp://localhost/alternate/audio"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, sent := startControlTestClient(t, &controlTestClient{}, controlTestSDP+"a=control:"+test.control+"\r\n")
			got := strings.SplitN((*sent)[2], "\r\n", 2)[0]
			if want := "SETUP " + test.uri + " RTSP/1.0"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

type udpFallbackTestClient struct{ controlTestClient }

func (*udpFallbackTestClient) HandleSetup(_ *RtspClient, res RtspResponse, track *RtspTrack, _ map[string]*RtspTrack, _ string, _ int) error {
	if res.StatusCode == Unsupported_Transport {
		track.SetTransport(NewRtspTransport(WithEnableUdp(), WithClientUdpPort(5000, 5001)))
	}
	return nil
}

func TestClientRetriesSetupForSameTrack(t *testing.T) {
	for _, test := range []struct {
		name       string
		extraMedia string
	}{
		{"single_track", ""},
		{"two_tracks", "m=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=control:video\r\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, sent := startControlTestClient(t, &udpFallbackTestClient{}, controlTestSDP+"a=control:audio\r\n"+test.extraMedia)
			feedControlMessage(t, client, "RTSP/1.0 461 Unsupported Transport\r\nCSeq: 3\r\n\r\n")
			if len(*sent) != 4 {
				t.Fatalf("expected SETUP retry, got %q", *sent)
			}
			retry := (*sent)[3]
			if !strings.HasPrefix(retry, "SETUP rtsp://localhost/source/audio RTSP/1.0\r\n") ||
				!strings.Contains(retry, "\r\nTransport: RTP/AVP/UDP;") {
				t.Fatalf("expected UDP SETUP for audio, got %q", retry)
			}
			feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 4\r\nSession: 12345678\r\n"+
				"Transport: RTP/AVP/UDP;unicast;client_port=5000-5001;server_port=6000-6001\r\n\r\n")
			want := "PLAY rtsp://localhost/source RTSP/1.0\r\n"
			if test.extraMedia != "" {
				want = "SETUP rtsp://localhost/source/video RTSP/1.0\r\n"
			}
			if len(*sent) != 5 || !strings.HasPrefix((*sent)[4], want) {
				t.Errorf("expected %q after successful retry, got %q", want, *sent)
			}
		})
	}
}

type rtpInfoTestClient struct {
	ClientHandle
	info *RtpInfo
}

func (handler *rtpInfoTestClient) HandlePlay(_ *RtspClient, _ RtspResponse, _ *RangeTime, info *RtpInfo) error {
	handler.info = info
	return nil
}

func TestPlayRTPInfoHeader(t *testing.T) {
	const value = "url=rtsp://localhost/live/trackID=1;seq=100;rtptime=9000"
	for _, name := range []string{"RTP-Info", "rtp-info", "RTPInfo"} {
		t.Run(name, func(t *testing.T) {
			handler := &rtpInfoTestClient{}
			client := &RtspClient{handle: handler}
			client.reponseHandler = client.handlePlay
			feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 4\r\n"+name+": "+value+"\r\n\r\n")
			want := RtpInfo{Url: "rtsp://localhost/live/trackID=1", Seq: 100, Rtptime: 9000}
			if handler.info == nil || *handler.info != want {
				t.Errorf("PLAY callback RTP-Info = %+v, want %+v", handler.info, want)
			}
		})
	}
	response := RtspResponse{Version: RTSP_1_0, StatusCode: 200, Fileds: HeadFiled{RTPInfo: value}}
	if wire := response.Encode(); !strings.Contains(wire, "\r\nRTP-Info: "+value+"\r\n") {
		t.Errorf("encoded PLAY response = %q", wire)
	}
}

func TestRTSPPlaysOneTrackWithoutAggregateControl(t *testing.T) {
	description := strings.Replace(controlTestSDP, "a=control:*\r\n", "", 1) + "a=control:audio\r\n"
	handler := &controlTestClient{}
	client, sent := startControlTestClient(t, handler, description)
	const trackURL = "rtsp://localhost/source/audio"
	if !strings.HasPrefix((*sent)[2], "SETUP "+trackURL+" RTSP/1.0\r\n") {
		t.Fatalf("SETUP did not use %q, got %q", trackURL, (*sent)[2])
	}
	feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 3\r\nSession: 12345678\r\n"+
		"Transport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n\r\n")
	if len(*sent) != 4 || !strings.HasPrefix((*sent)[3], "PLAY "+trackURL+" RTSP/1.0\r\n") {
		t.Fatalf("PLAY did not use %q, got %q", trackURL, *sent)
	}
	feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 4\r\nSession: 12345678\r\n\r\n")
	if client.state != STATE_Playing {
		t.Errorf("RTSP client state = %d, want Playing", client.state)
	}
}
