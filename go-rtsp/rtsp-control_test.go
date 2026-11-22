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
