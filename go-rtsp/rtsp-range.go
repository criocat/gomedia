package rtsp

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type RangeType int

const (
	RANGE_NPT RangeType = iota
	RANGE_UTC
)

const (
	nptNow          int64 = -1
	nptOmittedStart int64 = -2
	openRangeEnd    int64 = -1
	utcRangeLayout        = "20060102T150405.999Z"
)

type RangeTime struct {
	rangeType RangeType
	begin     int64
	end       int64
}

func (rt RangeTime) EncodeString() string {
	switch rt.rangeType {
	case RANGE_NPT:
		begin := ""
		switch rt.begin {
		case nptNow:
			begin = "now"
		case nptOmittedStart:
		default:
			begin = fmt.Sprintf("%d.%03d", rt.begin/1000, rt.begin%1000)
		}
		end := ""
		if rt.end != openRangeEnd {
			end = fmt.Sprintf("%d.%03d", rt.end/1000, rt.end%1000)
		}
		return "npt=" + begin + "-" + end
	case RANGE_UTC:
		begin := formatUTC(rt.begin)
		end := ""
		if rt.end != openRangeEnd {
			end = formatUTC(rt.end)
		}
		return "clock=" + begin + "-" + end
	default:
		panic("unsupported range time type")
	}
}

func formatUTC(millis int64) string {
	return time.Unix(millis/1000, millis%1000*int64(time.Millisecond)).UTC().Format(utcRangeLayout)
}

func parseUTC(clock string) (int64, error) {
	value, err := time.Parse(utcRangeLayout, clock)
	if err != nil {
		return 0, err
	}
	return value.Unix()*1000 + int64(value.Nanosecond())/int64(time.Millisecond), nil
}

func parseNPT(npt string) (int64, error) {
	const maxMillis = uint64(1<<63 - 1)
	var hours, minutes uint64
	fields := strings.Split(npt, ":")
	if len(fields) == 3 {
		var err error
		hours, err = strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return 0, err
		}
		minutes, err = strconv.ParseUint(fields[1], 10, 8)
		if err != nil || minutes > 59 {
			return 0, errors.New("invalid NPT minute")
		}
	} else if len(fields) != 1 {
		return 0, errors.New("invalid NPT time")
	}

	parts := strings.SplitN(fields[len(fields)-1], ".", 2)
	seconds, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || (len(fields) == 3 && seconds > 59) {
		return 0, errors.New("invalid NPT second")
	}
	seconds += minutes * 60
	var fraction uint64
	if len(parts) == 2 {
		for i, digit := range parts[1] {
			if digit < '0' || digit > '9' {
				return 0, errors.New("invalid NPT fraction")
			}
			if i < 3 {
				fraction = fraction*10 + uint64(digit-'0')
			}
		}
		for i := len(parts[1]); i < 3; i++ {
			fraction *= 10
		}
	}
	maxSeconds := (maxMillis - fraction) / 1000
	if seconds > maxSeconds || hours > (maxSeconds-seconds)/3600 {
		return 0, errors.New("NPT time out of range")
	}
	return int64((hours*3600+seconds)*1000 + fraction), nil
}

func parseRange(str string) (*RangeTime, error) {
	field := strings.SplitN(str, ";", 2)[0]
	parts := strings.SplitN(strings.TrimSpace(field), "=", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid Range header")
	}
	bounds := strings.SplitN(parts[1], "-", 2)
	rt := &RangeTime{end: openRangeEnd}
	var parse func(string) (int64, error)
	switch parts[0] {
	case "npt":
		rt.rangeType = RANGE_NPT
		parse = parseNPT
	case "clock":
		rt.rangeType = RANGE_UTC
		parse = parseUTC
	default:
		return nil, fmt.Errorf("unsupported Range type %q", parts[0])
	}

	var err error
	switch {
	case rt.rangeType == RANGE_NPT && bounds[0] == "now":
		rt.begin = nptNow
	case rt.rangeType == RANGE_NPT && bounds[0] == "" && len(bounds) == 2 && bounds[1] != "":
		rt.begin = nptOmittedStart
	default:
		rt.begin, err = parse(bounds[0])
		if err != nil {
			return nil, err
		}
	}
	if len(bounds) == 2 && bounds[1] != "" {
		rt.end, err = parse(bounds[1])
		if err != nil {
			return nil, err
		}
	}
	return rt, nil
}
