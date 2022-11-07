package bilibili

import (
	"strconv"
	"time"
)

func (this *BilibiliDownloader) speedSetBegin() {
	this.speedBytesLocker.Lock()
	defer this.speedBytesLocker.Unlock()

	this.speedBeginTime = time.Now()
}

func (this *BilibiliDownloader) speedAddBytes(a int) {
	this.speedBytesLocker.Lock()
	defer this.speedBytesLocker.Unlock()

	now := time.Now()
	this.speedBytesMap[now] += int64(a)
}

func (this *BilibiliDownloader) speedClearBytes() {
	this.speedBytesLocker.Lock()
	defer this.speedBytesLocker.Unlock()

	this.speedBytesMap = map[time.Time]int64{}
}

func (this *BilibiliDownloader) speedRecent5sGetAndUpdate() string {
	this.speedBytesLocker.Lock()
	defer this.speedBytesLocker.Unlock()

	now := time.Now()
	if this.isCancel() || this.speedBeginTime.IsZero() || now.Sub(this.speedBeginTime) < time.Second { // 1s以内, 暂时不计算速度
		return ""
	}

	const secondCount = 5

	expireTime := now.Add(-secondCount * time.Second)
	var total int64
	for ct, v := range this.speedBytesMap {
		if ct.Before(expireTime) {
			delete(this.speedBytesMap, ct)
			continue
		}
		total += v
	}
	realSecond := now.Sub(this.speedBeginTime).Seconds()
	if realSecond > secondCount {
		realSecond = secondCount
	}
	v := float64(total) / realSecond

	if v < 1024 {
		return strconv.Itoa(int(v)) + " B/s"
	}
	v = v / 1024
	if v < 1024 {
		return strconv.Itoa(int(v)) + " KB/s"
	}

	v = v / 1024
	return strconv.FormatFloat(v, 'f', 2, 64) + " MB/s"
}
