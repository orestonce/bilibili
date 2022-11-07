package bilibili

import (
	"fmt"
	"github.com/orestonce/gopool"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
}

func (this *BilibiliDownloader) DownloadVideoPart(req DownloadVideoPart_Req, aidPath string, curLength int64, totalLength int64, flvName *string) (err error) {
	const _startUrlTem = "https://api.bilibili.com/x/web-interface/view?aid=%d"
	referer := fmt.Sprintf(_startUrlTem, req.Aid)
	for i := int64(1); i <= req.Page; i++ {
		referer += fmt.Sprintf("/?p=%d", i)
	}
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		req.Header.Set("Referer", referer)
		return nil
	}}
	defer client.CloseIdleConnections()

	const splitSize = 512 * 1024

	var taskList []taskItem

	for begin := int64(0); begin < req.Size; begin += splitSize {
		end := begin + splitSize - 1
		if end >= req.Size {
			end = req.Size - 1
		}
		taskList = append(taskList, taskItem{
			retCh: make(chan taskResult, 1),
			begin: begin,
			end:   end,
		})
	}
	filename := fmt.Sprintf("%d_%d.flv", req.Page, req.Order)
	*flvName = filepath.Join(aidPath, filename)
	info, err := os.Stat(*flvName)
	if err == nil && info.Size() == req.Size {
		return nil
	}
	file, err := os.Create(*flvName)
	if err != nil {
		return err
	}
	defer file.Close()

	taskMgr := gopool.NewThreadPool(8)
	this.speedSetBegin()
	taskMgr.AddJob(func() {
		var cur int64
		for _, task := range taskList {
			ret := <-task.retCh
			if ret.err != nil {
				err = ret.err
				this.closeFn()
				return
			}
			_, err = file.Write(ret.content)
			if err != nil {
				this.closeFn()
				return
			}
			cur += int64(len(ret.content))
			FnUpdateProgress(float64(cur+curLength) / float64(totalLength))
			this.speedAddBytes(len(ret.content))
			FnMessage(this.speedRecent5sGetAndUpdate())
		}
	})
	for _, task := range taskList {
		task := task
		taskMgr.AddJob(func() {
			for i := 0; ; i++ {
				content, err0 := this.downloadRangeToMemory(client, referer, req, task.begin, task.end)
				if err0 != nil && i < 5 && this.isCancel() == false {
					FnMessage("下载错误, " + strconv.FormatInt(task.begin, 10) + ", " + err0.Error())
					this.sleepDur(time.Second * time.Duration(i+1))
					continue
				}
				task.retCh <- taskResult{
					content: content,
					err:     err0,
				}
				break
			}
		})
	}
	taskMgr.CloseAndWait()

	if err != nil {
		return err
	}
	err = file.Sync()
	if err != nil {
		return err
	}
	return file.Close()
}

type taskItem struct {
	retCh chan taskResult
	begin int64
	end   int64
}

type taskResult struct {
	content []byte
	err     error
}

func (this *BilibiliDownloader) downloadRangeToMemory(client *http.Client, referer string, req DownloadVideoPart_Req, begin int64, end int64) (content []byte, err error) {
	request, err := http.NewRequest("GET", req.UrlApi, nil)
	if err != nil {
		return nil, err
	}
	request = request.WithContext(this.ctx)
	rangeV := "bytes=" + strconv.FormatInt(begin, 10) + "-" + strconv.FormatInt(end, 10)

	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.13; rv:56.0) Gecko/20100101 Firefox/56.0")
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Accept-Language", "en-US,en;q=0.5")
	request.Header.Set("Accept-Encoding", "gzip, deflate, br")
	request.Header.Set("Range", rangeV)
	request.Header.Set("Referer", referer)
	request.Header.Set("Origin", "https://www.bilibili.com")
	request.Header.Set("Connection", "keep-alive")

	resp, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("错误码： %d", resp.StatusCode)
	}

	content, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if len(content) != int(end-begin)+1 {
		return content, fmt.Errorf("downloadRangeToMemory len invalid %d, %d", len(content), end-begin)
	}
	return content, nil
}
