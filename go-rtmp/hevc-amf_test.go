package rtmp

import (
	"bytes"
	"testing"
)

func TestHEVCConnectFourCC(t *testing.T) {
	_, objects := decodeAmf0(makeConnect("live", "rtmp://localhost/live"))
	if len(objects) != 1 {
		t.Fatalf("connect objects: %d", len(objects))
	}
	for _, item := range objects[0].items {
		if item.name == "fourCcList" {
			values, ok := item.value.value.([]amf0Item)
			if !ok || len(values) != 1 || string(values[0].value.([]byte)) != "hvc1" {
				t.Fatalf("wrong fourCcList: %+v", item)
			}
			return
		}
	}
	t.Fatal("missing fourCcList")
}

func TestHEVCAMFStrictArrayMalformed(t *testing.T) {
	valid := amf0Item{amfType: AMF0_STRICT_ARRAY, value: []amf0Item{makeStringItem("hvc1")}}
	wire := valid.encode()
	for n := 0; n < len(wire); n++ {
		var item amf0Item
		if consumed := item.decode(wire[:n]); consumed != 0 {
			t.Fatalf("truncated array consumed %d bytes (input %d)", consumed, n)
		}
	}
	for _, wire := range [][]byte{
		{10, 0xff, 0xff, 0xff, 0xff},
		{10, 0, 0, 0, 1, 0xff},
		{10, 0, 0, 0, 1, 2, 0, 4, 'h'},
		{10, 0, 0, 0, 1, 3, 0xff, 0xff},
	} {
		var item amf0Item
		if consumed := item.decode(wire); consumed != 0 {
			t.Fatalf("accepted malformed array %x", wire)
		}
	}
}

func TestHEVCConnectTruncatedFourCC(t *testing.T) {
	wire := makeConnect("live", "rtmp://localhost/live")
	server := NewRtmpServerHandle()
	server.SetOutput(func([]byte) error { return nil })
	field := bytes.Index(wire, []byte("fourCcList"))
	if field < 0 {
		t.Fatal("missing fourCcList")
	}
	for n := field + len("fourCcList"); n < len(wire)-len(EndObj); n++ {
		if err := server.handleCommand(wire[:n]); err == nil {
			t.Fatalf("accepted connect with truncated fourCcList at byte %d", n)
		}
	}
	if err := server.handleCommand(wire); err != nil {
		t.Fatalf("valid connect rejected: %v", err)
	}
}

func FuzzHEVCAMFStrictArray(f *testing.F) {
	f.Add([]byte{0, 0, 0, 1, 2, 0, 4, 'h', 'v', 'c', '1'})
	f.Add([]byte{0, 0, 0, 1, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) {
		var item amf0Item
		wire := append([]byte{byte(AMF0_STRICT_ARRAY)}, data...)
		n := item.decode(wire)
		if n < 0 || n > len(wire) {
			t.Fatalf("invalid consumed length %d for input of size %d", n, len(wire))
		}
	})
}
