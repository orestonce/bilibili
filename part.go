package bilibili

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

func GetFormatForExt(format string) string {
	if strings.HasPrefix(format, "flv") {
		value := format
		if value != "flv" {
			value = value + ".flv"
		}
		return value
	}
	return format
}

func (this *BilibiliDownloader) DownloadVideoPart(part VideoPart, outputNameFullPath string, curLength int64, totalLength int64) (err error) {
	info, err := os.Stat(outputNameFullPath)
	if err == nil && info.Size() == part.SizeValue { // 此文件已经下载了
		return nil
	}

	downloadingName := outputNameFullPath + ".downloading"
	var beginSize int64 = 0
	info, err = os.Stat(downloadingName)
	if err == nil && info.Size() <= part.SizeValue {
		beginSize = info.Size() // 正在下载, 还没下载完毕
	}
	var file *os.File
	if beginSize > 0 {
		file, err = os.OpenFile(downloadingName, os.O_RDWR, 0666)
	} else {
		file, err = os.Create(downloadingName)
	}
	if err != nil {
		return err
	}
	defer file.Close()

	if beginSize > 0 {
		_, err = file.Seek(beginSize, io.SeekStart)
		if err != nil {
			return err
		}
	}

	referer := part.Header.Get("Referer")
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		req.Header.Set("Referer", referer)
		return nil
	}}
	defer client.CloseIdleConnections()

	request, err := http.NewRequest(http.MethodGet, part.DownloadUrl, nil)
	if err != nil {
		return err
	}
	request = request.WithContext(this.ctx)
	request.Header = part.Header

	var resp *http.Response
	var isSingleThread bool
	if part.SizeValue-beginSize > 4*1024*1024 {
		resp, err = DoRequestMultThread(client, request, beginSize)
		isSingleThread = false
	} else {
		request.Header.Set("Range", "bytes="+strconv.FormatInt(beginSize, 10)+"-")
		resp, err = client.Do(request)
		isSingleThread = true
	}
	if err != nil {
		return err
	}
	this.speedSetBegin()

	pr := &progressReader{
		r:              resp.Body,
		curLength:      curLength + beginSize,
		totalLength:    totalLength,
		downloader:     this,
		ticker:         time.NewTicker(time.Millisecond * 100),
		isSingleThread: isSingleThread,
	}
	defer pr.ticker.Stop()

	_, err = io.Copy(file, pr)
	_ = resp.Body.Close()
	if err != nil {
		return err
	}

	err = file.Sync()
	if err != nil {
		return err
	}
	err = file.Close()
	if err != nil {
		return err
	}
	return os.Rename(downloadingName, outputNameFullPath)
}

type progressReader struct {
	r       io.Reader
	n       int64
	nLocker sync.Mutex

	curLength      int64
	totalLength    int64
	downloader     *BilibiliDownloader
	ticker         *time.Ticker
	isSingleThread bool
}

func (this *progressReader) Read(buf []byte) (n int, err error) {
	n, err = this.r.Read(buf)

	if err != nil {
		return n, err
	}
	this.nLocker.Lock()
	this.n += int64(n)
	value := this.curLength + this.n
	this.nLocker.Unlock()

	FnUpdateProgress(float64(value) / float64(this.totalLength))
	this.downloader.speedAddBytes(n)

	select {
	case <-this.ticker.C:
		speed := this.downloader.speedRecent5sGetAndUpdate()
		if speed != "" {
			vt := "(1)"
			if this.isSingleThread == false {
				vt = "(n)"
			}
			FnMessage("下载速度" + vt + ": " + speed)
		}
	default:
	}
	return n, nil
}
