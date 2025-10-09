package utils

import "time"

// ShanghaiLocation 东八区时区（中国标准时间）
var ShanghaiLocation *time.Location

func init() {
	var err error
	ShanghaiLocation, err = time.LoadLocation("Asia/Shanghai")
	if err != nil {
		// 如果加载失败，使用固定偏移量（UTC+8）
		ShanghaiLocation = time.FixedZone("CST", 8*3600)
	}
}

// Now 返回东八区的当前时间
func Now() time.Time {
	return time.Now().In(ShanghaiLocation)
}

// Unix 将 Unix 时间戳转换为东八区时间
func Unix(sec int64, nsec int64) time.Time {
	return time.Unix(sec, nsec).In(ShanghaiLocation)
}

// ToShanghai 将任意时间转换为东八区时间
func ToShanghai(t time.Time) time.Time {
	return t.In(ShanghaiLocation)
}
