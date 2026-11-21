package rtmp

import (
	"bytes"
	"testing"
)

func TestHEVCConnectFourCC(t *testing.T) {
	name, args, err := decodeCommand(makeConnect("live", "rtmp://localhost/live"))
	if err != nil || name != "connect" || len(args) != 2 {
		t.Fatalf("connect command: %q, %v, %v", name, args, err)
	}
	for _, item := range args[1].value.(amfObject).items {
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
	for n := field + len("fourCcList"); n < len(wire); n++ {
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

func TestAMF0NestedValues(t *testing.T) {
	object := amfObject{items: []*amfObjectItem{{name: "codec", value: makeStringItem("hvc1")}}}.encode()
	ecma := append([]byte{byte(AMF0_ECMA_ARRAY), 0, 0, 0, 0}, object[1:]...)
	for _, container := range [][]byte{object, ecma} {
		wire := append([]byte{byte(AMF0_STRICT_ARRAY), 0, 0, 0, 2}, container...)
		wire = append(wire, makeBoolItem(true).encode()...)
		var item amf0Item
		for n := range wire {
			if item.decode(wire[:n]) != 0 {
				t.Fatalf("accepted prefix %d of %x", n, wire)
			}
		}
		if n := item.decode(wire); n != len(wire) {
			t.Fatalf("consumed %d of %d bytes", n, len(wire))
		}
		values := item.value.([]amf0Item)
		property := values[0].value.(amfObject).items[0]
		if property.name != "codec" || string(property.value.value.([]byte)) != "hvc1" || values[1].value != true {
			t.Fatal("incorrect nested properties or following value")
		}
	}
	deep := bytes.Repeat([]byte{byte(AMF0_STRICT_ARRAY), 0, 0, 0, 1}, 33)
	deep = append(deep, NullItem...)
	var item amf0Item
	if item.decode(deep[5:]) != len(deep)-5 || item.decode(deep) != 0 {
		t.Fatal("incorrect nesting limit")
	}
}

func TestAMF0CommandArguments(t *testing.T) {
	number := makeNumberItem(1).encode()
	str := makeStringItem("stream").encode()
	flag := makeBoolItem(true).encode()
	status := amfObjectWithNumber("code")
	for _, tc := range []struct {
		name     string
		response bool
		args     [][]byte
	}{
		{"connect", false, [][]byte{number, amfObjectWithNumber("app")}},
		{"connect", false, [][]byte{number, amfObjectWithNumber("tcUrl")}},
		{"connect", false, [][]byte{number, {byte(AMF0_OBJECT)}}},
		{"createStream", false, [][]byte{str}},
		{"releaseStream", false, [][]byte{number, NullItem, flag}},
		{"publish", false, [][]byte{number, NullItem}},
		{"play", false, [][]byte{number, NullItem, str, flag}},
		{"play", false, [][]byte{number, NullItem, str, number, flag}},
		{"play", false, [][]byte{number, NullItem, str, number, number, str}},
		{"onStatus", true, [][]byte{number, NullItem, status}},
		{"_error", true, [][]byte{number, NullItem, status}},
		{"_result", true, [][]byte{str}},
		{"_result", true, [][]byte{number, NullItem, str}},
		{"_result", true, [][]byte{number, NullItem, {byte(AMF0_ECMA_ARRAY)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := append(makeStringItem(tc.name).encode(), bytes.Join(tc.args, nil)...)
			server := NewRtmpServerHandle()
			client := NewRtmpClient()
			client.lastMethod = CREATE_STREAM
			client.lastMethodTid = 1
			output := func([]byte) error {
				t.Fatal("malformed command produced output")
				return nil
			}
			server.SetOutput(output)
			client.SetOutput(output)
			handle := server.handleCommand
			if tc.response {
				handle = client.handleCommandRes
			}
			if err := handle(wire); err == nil {
				t.Fatal("accepted invalid AMF0 command")
			}
		})
	}
}

func amfObjectWithNumber(name string) []byte {
	return amfObject{items: []*amfObjectItem{{name: name, value: makeNumberItem(1)}}}.encode()
}

func TestAMF0ScalarLengths(t *testing.T) {
	for _, wire := range [][]byte{
		makeNumberItem(1).encode(),
		makeBoolItem(true).encode(),
		makeStringItem("a").encode(),
		{byte(AMF0_LONG_STRING), 0, 0, 0, 1, 'a'},
	} {
		var item amf0Item
		for n := range wire {
			if item.decode(wire[:n]) != 0 {
				t.Fatalf("accepted prefix %d of %x", n, wire)
			}
		}
		if item.decode(wire) != len(wire) {
			t.Fatalf("rejected complete value %x", wire)
		}
		if item.decode(NullItem) != 1 || item.value != nil || item.length != 0 {
			t.Fatal("null retained the previous value")
		}
	}
}

func TestAMF0ErrorDescription(t *testing.T) {
	cli := NewRtmpClient()
	calls := 0
	cli.OnError(func(code, description string) {
		calls++
		if code != "NetConnection.Connect.Rejected" || description != "Rejected" {
			t.Fatalf("unexpected error properties %q, %q", code, description)
		}
	})
	if err := cli.handleCommandRes(makeErrorRes(1, "error", "NetConnection.Connect.Rejected", "Rejected")); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected one error callback, got %d", calls)
	}
}
