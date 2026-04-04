package utils

import "time"

func ResolveTime(startStr, endStr string) (time.Time, time.Time) {
	now := time.Now()
	loc := now.Location() // 默认使用本地时区

	var startTime, endTime time.Time
	var err error

	// 解析 EndTime
	if endStr != "" {
		endTime, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			endTime = now // 解析失败就用“现在”兜底
		}
	} else {
		endTime = now // 如果没给结束时间，默认查到“现在”
	}

	// 解析 StartTime
	if startStr != "" {
		startTime, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			// 解析失败，默认从“一个月前”开始查
			startTime = now.AddDate(0, -1, 0)
		}
	} else {
		// 如果用户完全没给时间范围，默认只查“最近 7 天”
		startTime = now.AddDate(0, 0, -7)
	}

	// 确保时区一致
	return startTime.In(loc), endTime.In(loc)
}
