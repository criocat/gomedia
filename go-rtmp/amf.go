package rtmp

import (
	"encoding/binary"
	"errors"
	"math"
)

type AMF0_DATA_TYPE int

const (
	AMF0_NUMBER AMF0_DATA_TYPE = iota
	AMF0_BOOLEAN
	AMF0_STRING
	AMF0_OBJECT
	AMF0_MOVIECLIP
	AMF0_NULL
	AMF0_UNDEFINED
	AMF0_REFERENCE
	AMF0_ECMA_ARRAY
	AMF0_OBJECT_END
	AMF0_STRICT_ARRAY
	AMF0_DATE
	AMF0_LONG_STRING
	AMF0_UNSUPPORTED
	AMF0_RECORDSET
	AMF0_XML_DOCUMENT
	AMF0_TYPED_OBJECT
	AMF0_AVMPLUS_OBJECT
)

var NullItem []byte = []byte{byte(AMF0_NULL)}
var EndObj []byte = []byte{0, 0, byte(AMF0_OBJECT_END)}

type amf0Item struct {
	amfType AMF0_DATA_TYPE
	length  int
	value   interface{}
}

func (amf amf0Item) encode() []byte {
	buf := make([]byte, amf.length+4+8)
	switch amf.amfType {
	case AMF0_NUMBER:
		buf[0] = byte(AMF0_NUMBER)
		binary.BigEndian.PutUint64(buf[1:], math.Float64bits(amf.value.(float64)))
		return buf[:9]
	case AMF0_BOOLEAN:
		buf[0] = byte(AMF0_BOOLEAN)
		v := amf.value.(bool)
		if v {
			buf[1] = 1
		} else {
			buf[1] = 0
		}
		return buf[0:2]
	case AMF0_STRING:
		buf[0] = byte(AMF0_STRING)
		buf[1] = byte(uint16(amf.length) >> 8)
		buf[2] = byte(uint16(amf.length))
		copy(buf[3:], []byte(amf.value.(string)))
		return buf[0 : 3+amf.length]
	case AMF0_MOVIECLIP:
	case AMF0_NULL:
		buf[0] = byte(AMF0_NULL)
		return buf[0:1]
	case AMF0_UNDEFINED:
	case AMF0_REFERENCE:
	case AMF0_ECMA_ARRAY:
	case AMF0_STRICT_ARRAY:
		values := amf.value.([]amf0Item)
		data := make([]byte, 5)
		data[0] = byte(AMF0_STRICT_ARRAY)
		binary.BigEndian.PutUint32(data[1:], uint32(len(values)))
		for _, value := range values {
			data = append(data, value.encode()...)
		}
		return data
	case AMF0_DATE:
	case AMF0_LONG_STRING:
	case AMF0_UNSUPPORTED:
	case AMF0_RECORDSET:
	case AMF0_XML_DOCUMENT:
	case AMF0_TYPED_OBJECT:
	case AMF0_AVMPLUS_OBJECT:
	default:
		panic("unsupport")
	}
	return nil
}

func (amf *amf0Item) decode(data []byte) int {
	return amf.decodeAt(data, 0)
}

func (amf *amf0Item) decodeAt(data []byte, depth int) int {
	if len(data) == 0 || depth > 32 {
		return 0
	}
	*amf = amf0Item{amfType: AMF0_DATA_TYPE(data[0])}
	switch amf.amfType {
	case AMF0_NUMBER:
		if len(data) < 9 {
			return 0
		}
		amf.value = math.Float64frombits(binary.BigEndian.Uint64(data[1:]))
		amf.length = 8
		return 9
	case AMF0_BOOLEAN:
		if len(data) < 2 {
			return 0
		}
		amf.value = data[1] == 1
		amf.length = 1
		return 2
	case AMF0_NULL, AMF0_UNDEFINED:
		return 1
	case AMF0_STRING, AMF0_LONG_STRING:
		pos := 3
		if len(data) < pos {
			return 0
		}
		size := uint32(binary.BigEndian.Uint16(data[1:]))
		if amf.amfType == AMF0_LONG_STRING {
			pos = 5
			if len(data) < pos {
				return 0
			}
			size = binary.BigEndian.Uint32(data[1:])
		}
		if uint64(size) > uint64(len(data)-pos) {
			return 0
		}
		amf.length = int(size)
		amf.value = append([]byte(nil), data[pos:pos+amf.length]...)
		return pos + amf.length
	case AMF0_OBJECT, AMF0_ECMA_ARRAY:
		pos := 1
		if amf.amfType == AMF0_ECMA_ARRAY {
			pos = 5 // The property count is advisory.
		}
		var object amfObject
		for len(data)-pos >= 3 {
			size := int(binary.BigEndian.Uint16(data[pos:]))
			if size == 0 && data[pos+2] == byte(AMF0_OBJECT_END) {
				amf.value = object
				return pos + 3
			}
			pos += 2
			if size > len(data)-pos {
				return 0
			}
			property := &amfObjectItem{name: string(data[pos : pos+size])}
			pos += size
			n := property.value.decodeAt(data[pos:], depth+1)
			if n == 0 {
				return 0
			}
			object.items = append(object.items, property)
			pos += n
		}
	case AMF0_STRICT_ARRAY:
		if len(data) < 5 {
			return 0
		}
		count := binary.BigEndian.Uint32(data[1:])
		pos := 5
		if uint64(count) > uint64(len(data)-pos) {
			return 0
		}
		var values []amf0Item
		for i := uint32(0); i < count; i++ {
			var item amf0Item
			n := item.decodeAt(data[pos:], depth+1)
			if n == 0 {
				return 0
			}
			pos += n
			values = append(values, item)
		}
		amf.value = values
		return pos
	}
	return 0
}

func decodeCommand(data []byte) (string, []amf0Item, error) {
	var name amf0Item
	n := name.decode(data)
	if n == 0 || name.amfType != AMF0_STRING {
		return "", nil, errors.New("invalid AMF0 command name")
	}
	var args []amf0Item
	for data = data[n:]; len(data) > 0; data = data[n:] {
		var arg amf0Item
		n = arg.decode(data)
		if n == 0 {
			return "", nil, errors.New("invalid AMF0 command argument")
		}
		args = append(args, arg)
	}
	return string(name.value.([]byte)), args, nil
}

func makeStringItem(str string) amf0Item {
	item := amf0Item{
		amfType: AMF0_STRING,
		length:  len(str),
		value:   str,
	}
	return item
}

func makeNumberItem(num float64) amf0Item {
	item := amf0Item{
		amfType: AMF0_NUMBER,
		value:   num,
	}
	return item
}

func makeBoolItem(v bool) amf0Item {
	item := amf0Item{
		amfType: AMF0_BOOLEAN,
		value:   v,
	}
	return item
}

type amfObjectItem struct {
	name  string
	value amf0Item
}

type amfObject struct {
	items []*amfObjectItem
}

func (object amfObject) encode() []byte {
	obj := make([]byte, 1)
	obj[0] = byte(AMF0_OBJECT)
	for _, item := range object.items {
		lenbytes := make([]byte, 2)
		binary.BigEndian.PutUint16(lenbytes, uint16(len(item.name)))
		obj = append(obj, lenbytes...)
		obj = append(obj, []byte(item.name)...)
		obj = append(obj, item.value.encode()...)
	}
	obj = append(obj, EndObj...)
	return obj
}
