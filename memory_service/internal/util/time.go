package util

import "time"

func NowMilli() int64 {
	return time.Now().UnixMilli()
}
