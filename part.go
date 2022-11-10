package bilibili

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DownloadVideoPart_Req struct {
	UrlApi string
	Size   int64
	Title  string
	Aid    int64
	Page   int64
	Cid    int64
	Part   string
	Order  int64
	Format string
}

func (r DownloadVideoPart_Req) GetFormatForExt() string {
	if strings.HasPrefix(r.Format, "flv") {
		value := r.Format
		if value != "flv" {
			value = value + ".flv"
		}
		return value
	}
	return r.Format
}

func (this *BilibiliDownloader) DownloadVideoPart(req DownloadVideoPart_Req, onlyOne bool, aidPath string, curLength int64, totalLength int64, flvName *string) (err error) {
	if onlyOne {
		*flvName = aidPath + "." + req.GetFormatForExt()
	} else {
		*flvName = filepath.Join(aidPath, fmt.Sprintf("%d_%d_%s.%s", req.Page, req.Order, TitleEdit(req.Title), req.GetFormatForExt()))
	}
	info, err := os.Stat(*flvName)
	if err == nil && info.Size() == req.Size { // 此文件已经下载了
		return nil
	}

	downloadingName := *flvName + ".downloading"
	var beginSize int64 = 0
	info, err = os.Stat(downloadingName)
	if err == nil && info.Size() <= req.Size {
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

	referer := fmt.Sprintf("https://api.bilibili.com/x/web-interface/view?aid=%d", req.Aid)
	for i := int64(1); i <= req.Page; i++ {
		referer += fmt.Sprintf("/?p=%d", i)
	}
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		req.Header.Set("Referer", referer)
		return nil
	}}
	defer client.CloseIdleConnections()

	request, err := http.NewRequest(http.MethodGet, req.UrlApi, nil)
	if err != nil {
		return err
	}
	request = request.WithContext(this.ctx)
	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.13; rv:56.0) Gecko/20100101 Firefox/56.0")
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Accept-Language", "en-US,en;q=0.5")
	request.Header.Set("Accept-Encoding", "gzip, deflate, br")
	request.Header.Set("Referer", referer)
	request.Header.Set("Origin", "https://www.bilibili.com")
	request.Header.Set("Connection", "keep-alive")

	var resp *http.Response
	var isSingleThread bool
	if req.Size-beginSize > 4*1024*1024 {
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
		curLength:      curLength,
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
	return os.Rename(downloadingName, *flvName)
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
