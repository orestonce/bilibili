package bilibili

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
)

func DoRequestMultThread(client *http.Client, req *http.Request, beginSize int64) (resp *http.Response, err error) {
	if s := req.Header.Get("Range"); s != "" {
		return nil, errors.New(`DoRequestMultThread not support [Range] header`)
	}

	var body []byte
	if req.Body != nil {
		body, err = ioutil.ReadAll(req.Body)
		if err != nil {
			req.Body.Close()
			return nil, err
		}
		req.Body.Close()
		req.Body = ioutil.NopCloser(bytes.NewBuffer(body))
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Range", "bytes=0-0") // 读第一个字节
	resp0, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	var totalSize int64
	if rs := resp0.Header.Get("Content-Range"); rs == "" { // 服务端不支持 Range
		resp0.Body.Close()
		return nil, errors.New("DoRequestMultThread server unsupported 'Range' header")
	} else if strings.HasPrefix(rs, "bytes 0-0/") == false {
		resp0.Body.Close()
		return nil, errors.New(`DoRequestMultThread unexpected Content-Range ` + strconv.Quote(rs))
	} else {
		resp0.Body.Close()
		r := strings.TrimPrefix(rs, `bytes 0-0/`)
		totalSize, err = strconv.ParseInt(r, 10, 64)
		if err != nil {
			return nil, err
		}
	}
	ctx, cancelFn := context.WithCancel(req.Context())

	var workerList []*workerItem
	for i := 0; i < 8; i++ {
		var item = &workerItem{
			taskIn:    make(chan taskItem, 1), // 分配任务不阻塞
			resultOut: make(chan resultItem),  // 阻塞的获取执行结果
		}
		workerList = append(workerList, item)
	}

	go threadDispatchTask(ctx, beginSize, totalSize, workerList)

	for _, item := range workerList {
		go threadWorkerRun(ctx, cancelFn, client, item, req, body)
	}

	pr, pw := io.Pipe()
	go threadCollectResult(ctx, cancelFn, workerList, pw)

	resp = resp0
	resp.Header.Set("Content-Range", `bytes `+strconv.FormatInt(beginSize, 10)+`-`+strconv.FormatInt(totalSize, 10)+`/`+strconv.FormatInt(totalSize, 10))
	resp.Body = pr

	return resp, nil
}

func threadCollectResult(ctx context.Context, cancelFn context.CancelFunc, list []*workerItem, pw *io.PipeWriter) {
	for cIndex := 0; ; cIndex = (cIndex + 1) % len(list) {
		item := list[cIndex]
		select {
		case result, ok := <-item.resultOut:
			if ok == false { // 正常退出
				item.isDone = true
				var allItemDone = true
				for _, other := range list {
					if other.isDone == false {
						allItemDone = false
						break
					}
				}
				if allItemDone {
					pw.Close()
					return
				}
				continue
			}
			if result.err != nil {
				pw.CloseWithError(result.err)
				cancelFn()
				return
			}
			_, err := pw.Write(result.data)
			if err != nil {
				pw.CloseWithError(err)
				cancelFn()
				return
			}
		case <-ctx.Done():
			pw.CloseWithError(ctx.Err())
			return
		}
	}
}

func threadWorkerRun(ctx context.Context, cancelFn context.CancelFunc, client *http.Client, item *workerItem, originReq *http.Request, body []byte) {
	writeResp := func(data []byte, err error) {
		result := resultItem{
			data: data,
			err:  err,
		}
		select {
		case <-ctx.Done():
		case item.resultOut <- result:
		}
		if err != nil {
			cancelFn()
		}
	}

	for {
		var task taskItem
		var ok bool
		select {
		case <-ctx.Done():
			return
		case task, ok = <-item.taskIn:
			if ok == false {
				close(item.resultOut)
				return
			}
		}
		req := &http.Request{
			Method:     originReq.Method,
			URL:        originReq.URL,
			Proto:      "HTTP/1.1",
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header:     make(http.Header),
			Body:       ioutil.NopCloser(bytes.NewReader(body)),
			Host:       originReq.URL.Host,
		}
		req = req.WithContext(ctx)
		for k, vList := range originReq.Header {
			req.Header[k] = vList
		}

		req.Header.Set("Range", "bytes="+strconv.FormatInt(task.begin, 10)+"-"+strconv.FormatInt(task.end, 10))
		resp, err := client.Do(req)
		if err != nil {
			writeResp(nil, err)
			return
		}
		if resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			writeResp(nil, fmt.Errorf("错误码： %d", resp.StatusCode))
			return
		}
		data, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			resp.Body.Close()
			writeResp(nil, err)
			return
		}
		writeResp(data, nil)
	}
}

func threadDispatchTask(ctx context.Context, beginSize int64, totalSize int64, outWorker []*workerItem) {
	writeIdx := 0

	for begin := beginSize; begin < totalSize; begin += splitSize {
		end := begin + splitSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}
		task := taskItem{
			begin: begin,
			end:   end,
		}
		select {
		case outWorker[writeIdx].taskIn <- task:
		case <-ctx.Done():
		}
		writeIdx = (writeIdx + 1) % len(outWorker)
	}
	for _, item := range outWorker {
		close(item.taskIn)
	}
}

type workerItem struct {
	taskIn    chan taskItem
	resultOut chan resultItem
	isDone    bool
}

type taskItem struct {
	begin int64
	end   int64
}

type resultItem struct {
	data []byte
	err  error
}

const splitSize = 512 * 1024 // 512KB

//type getRangeResp struct {
//	begin    int64
//	hasBegin bool
//	end      int64
//	hasEnd   bool
//}

//func getRange(v string) (resp *getRangeResp, err error) {
//	v = strings.TrimSpace(v)
//	if strings.HasPrefix(v, `bytes=`) == false {
//		return nil, errors.New(`getRange format error ` + strconv.Quote(v))
//	}
//	tmp := strings.Split(strings.TrimPrefix(v, "bytes="), "-")
//	if len(tmp) != 2 || (tmp[0] == "" && tmp[1] == "") {
//		return nil, errors.New(`getRange format error2 ` + strconv.Quote(v))
//	}
//	resp = &getRangeResp{}
//	if tmp[0] != "" {
//		resp.hasBegin = true
//		resp.begin, err = strconv.ParseInt(tmp[1], 10, 64)
//		if err != nil {
//			return nil, err
//		}
//	}
//	if tmp[1] != "" {
//		resp.hasEnd = true
//		resp.end, err = strconv.ParseInt(tmp[1], 10, 64)
//		if err != nil {
//			return nil, err
//		}
//	}
//	return resp, nil
//}
