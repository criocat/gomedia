package rtmp

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestServerHandshakeReturnsOutputError(t *testing.T) {
	server := NewRtmpServerHandle()
	calls := 0
	server.SetOutput(func([]byte) error {
		calls++
		return io.ErrClosedPipe
	})
	request := append(makeC0(), makeC1()...)
	if err := server.Input(request[:37]); err != nil || calls != 0 {
		t.Fatalf("partial C0/C1: err = %v, writes = %d", err, calls)
	}
	if err := server.Input(request[37:]); !errors.Is(err, io.ErrClosedPipe) || calls != 1 {
		t.Errorf("complete C0/C1: err = %v, writes = %d, want one failed write", err, calls)
	}
}

func TestComplexClientEchoesSimpleServerHandshake(t *testing.T) {
	for _, name := range []string{"simple_server", "complex_server"} {
		t.Run(name, func(t *testing.T) {
			client := NewRtmpClient(WithComplexHandshake())
			var sent [][]byte
			client.SetOutput(func(data []byte) error {
				sent = append(sent, append([]byte(nil), data...))
				return nil
			})
			client.Start("rtmp://localhost/live/test")
			if len(sent) != 1 || len(sent[0]) != 1+HANDSHAKE_SIZE {
				t.Fatalf("expected one C0/C1 block, got %d writes", len(sent))
			}
			c1 := sent[0][1:]
			s1, s2 := makeS1(), makeS2(c1)
			if name == "complex_server" {
				s1, s2 = makeComplexS1(client.hs.schema), makeComplexS2(c1, client.hs.schema)
			}
			response := append(append(makeS0(), s1...), s2...)
			if err := client.Input(response[:31]); err != nil {
				t.Fatal(err)
			}
			if err := client.Input(response[31:]); err != nil {
				t.Fatal(err)
			}
			if len(sent) < 2 || len(sent[1]) != HANDSHAKE_SIZE {
				t.Fatal("missing C2")
			}
			echo := bytes.Equal(sent[1][:4], s1[:4]) && bytes.Equal(sent[1][8:], s1[8:])
			if echo != (name == "simple_server") {
				t.Errorf("C2 echoes the S1 timestamp and random bytes: %v", echo)
			}
		})
	}
}
