package rtcp

import "time"

const ntpUnixEpochOffset = 2208988800

// NTP2UtcClock interprets a timestamp in NTP era 0 (1900–2036).
func NTP2UtcClock(ntp uint64) time.Time {
    seconds := int64(ntp>>32) - ntpUnixEpochOffset
    fraction := uint64(uint32(ntp))
    nanoseconds := (fraction*uint64(time.Second) + 1<<31) >> 32
    return time.Unix(seconds, int64(nanoseconds)).UTC()
}

func UtcClockToNTP(t time.Time) uint64 {
    seconds := uint64(t.Unix() + ntpUnixEpochOffset)
    fraction := uint64(t.Nanosecond()) << 32 / uint64(time.Second)
    return seconds<<32 | fraction
}
