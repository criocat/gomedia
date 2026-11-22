package rtcp

import (
	"bytes"
	"testing"
)

func TestAppPacketSubtypeAndData(t *testing.T) {
	want := []byte{0x83, 204, 0, 3, 1, 2, 3, 4, 'T', 'E', 'S', 'T', 9, 8, 7, 6}
	app := NewApp()
	app.SubType = 3
	app.SSRC = 0x01020304
	app.Name = []byte("TEST")
	app.AppData = []byte{9, 8, 7, 6}
	if got := app.Encode(); !bytes.Equal(got, want) {
		t.Errorf("encoded APP = %x, want %x", got, want)
	}

	for _, test := range []struct {
		name string
		wire []byte
	}{
		{"standalone", want},
		{"following_packet", append(append([]byte(nil), want...), 0x80, 201, 0, 1, 0, 0, 0, 7)},
		{"padded", []byte{0xa3, 204, 0, 4, 1, 2, 3, 4, 'T', 'E', 'S', 'T', 9, 8, 7, 6, 0, 0, 0, 4}},
	} {
		t.Run(test.name, func(t *testing.T) {
			packet := NewApp()
			if err := packet.Decode(test.wire); err != nil {
				t.Fatal(err)
			}
			if packet.SubType != 3 || packet.SSRC != 0x01020304 || string(packet.Name) != "TEST" ||
				!bytes.Equal(packet.AppData, []byte{9, 8, 7, 6}) {
				t.Errorf("unexpected decoded APP: %+v", packet)
			}
			end := 4 + 4*int(packet.Length)
			if got := packet.Encode(); !bytes.Equal(got, test.wire[:end]) {
				t.Errorf("re-encoded APP = %x, want %x", got, test.wire[:end])
			}
		})
	}
}

func TestAppPacketWithoutApplicationData(t *testing.T) {
	wire := []byte{0x80, 204, 0, 2, 1, 2, 3, 4, 'T', 'E', 'S', 'T'}
	app := NewApp()
	if err := app.Decode(wire); err != nil {
		t.Fatal(err)
	}
	if len(app.AppData) != 0 || string(app.Name) != "TEST" {
		t.Errorf("unexpected decoded APP: %+v", app)
	}
}
