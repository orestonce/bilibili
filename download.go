package bilibili

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type BilibiliDownloader struct {
	ctx     context.Context
	closeFn func()
	req     BeginDownload_Req
}

var gDownloader *BilibiliDownloader
var gDownloaderLocker sync.Mutex

type BeginDownload_Req struct {
	Url     string
	SaveDir string
}

type PrintFnS struct {
	FnError          func(errMsg string)
	FnMessage        func(msg string)
	FnUpdateProgress func(d float64)
	FnUpdateRunning  func(b bool)
	FnDownloadFinish func(outMp4File string)
}

func InitPrintFnS(req PrintFnS) {
	gPrintFnS = &req
}

var gPrintFnS *PrintFnS
var gPrintFnSLocker sync.Mutex

func FnError(errMsg string) {
	gPrintFnSLocker.Lock()
	defer gPrintFnSLocker.Unlock()
	if gPrintFnS == nil || gPrintFnS.FnError == nil {
		return
	}
	gPrintFnS.FnError(errMsg)
}

func FnMessage(msg string) {
	gPrintFnSLocker.Lock()
	defer gPrintFnSLocker.Unlock()
	if gPrintFnS == nil || gPrintFnS.FnMessage == nil {
		return
	}
	gPrintFnS.FnMessage(msg)
}

func FnUpdateProgress(d float64) {
	gPrintFnSLocker.Lock()
	defer gPrintFnSLocker.Unlock()
	if gPrintFnS == nil || gPrintFnS.FnUpdateProgress == nil {
		return
	}
	gPrintFnS.FnUpdateProgress(d)
}

func FnUpdateRunning(b bool) {
	gPrintFnSLocker.Lock()
	defer gPrintFnSLocker.Unlock()
	if gPrintFnS == nil || gPrintFnS.FnUpdateRunning == nil {
		return
	}
	gPrintFnS.FnUpdateRunning(b)
}

func FnDownloadFinish(outMp4File string) {
	gPrintFnSLocker.Lock()
	defer gPrintFnSLocker.Unlock()
	if gPrintFnS == nil || gPrintFnS.FnDownloadFinish == nil {
		return
	}
	gPrintFnS.FnDownloadFinish(outMp4File)
}

var gRunningThreadCount int64
var gRunningThreadCountLocker sync.Mutex

func BeginDownloadAsync(req BeginDownload_Req) {
	gRunningThreadCountLocker.Lock()
	gRunningThreadCount++
	if gRunningThreadCount == 1 {
		FnUpdateRunning(true)
	}
	gRunningThreadCountLocker.Unlock()

	tmp := &BilibiliDownloader{
		req: req,
	}
	tmp.ctx, tmp.closeFn = context.WithCancel(context.Background())

	gDownloaderLocker.Lock()
	if gDownloader != nil {
		gDownloader.closeFn()
	}
	gDownloader = tmp
	gDownloaderLocker.Unlock()

	go tmp.RunDownload()
}

func StopDownload() {
	gDownloaderLocker.Lock()
	if gDownloader != nil {
		gDownloader.closeFn()
	}
	gDownloaderLocker.Unlock()
}

func (this *BilibiliDownloader) GetVideoInfoListV2(urlInput string) (resp GetVideoInfoList_Resp) {
	//if params := regexp.MustCompile(`space.bilibili.com/(\d+)/?`).FindStringSubmatch(urlInput); len(params) > 0 {
	//	upId, _ := strconv.ParseInt(params[1], 10, 64)
	//	return getVideoInfoList_ByUpId(upId)
	//} else
	if params := regexp.MustCompile(`/?(BV\w+)[/?]?`).FindStringSubmatch(urlInput); params != nil {
		aid := Bv2av(params[1])
		return this.getVideoInfoList_ByAidV2(aid)
	} else if params = regexp.MustCompile(`/?(av\d+)/?`).FindStringSubmatch(urlInput); len(params) > 0 {
		aid, _ := strconv.ParseInt(strings.TrimPrefix(params[1], "av"), 10, 64)
		return this.getVideoInfoList_ByAidV2(aid)
	}
	resp.ErrMsg = "您输入的网址无法解析"
	return resp
}

// source code: https://blog.csdn.net/dotastar00/article/details/108805779
func Bv2av(x string) int64 {
	const table = "fZodR9XQDSUm21yCkr6zBqiveYah8bt4xsWpHnJE7jL5VG3guMTKNPAwcF"
	var s = [6]int{11, 10, 3, 8, 4, 6}
	const xor = 177451812
	const add = 8728348608

	tr := make(map[string]int)
	for i := 0; i < 58; i++ {
		tr[string(table[i])] = i
	}
	r := 0
	for i := 0; i < 6; i++ {
		r += tr[string(x[s[i]])] * int(math.Pow(float64(58), float64(i)))
	}
	return int64((r - add) ^ xor)
}

const _getCidUrlTemp = "https://api.bilibili.com/x/web-interface/view?aid=%d"
const _entropy = "rbMCKn@KuamXWlPMoJGsKcbiJKUfkPF_8dABscJntvqhRSETg"
const _paramsTemp = "appkey=%s&cid=%s&otype=json&qn=%s&quality=%s&type="
const _playApiTemp = "https://interface.bilibili.com/v2/playurl?%s&sign=%s"
const _quality = "80"

type videoCid struct {
	Aid    int64
	Cid    int64
	Page   int64
	Part   string
	UrlApi string
	L1Data cidL1
}

type cidL1 struct {
	From              string   `json:"from"`
	Result            string   `json:"result"`
	Quality           int      `json:"quality"`
	Format            string   `json:"format"`
	Timelength        int      `json:"timelength"`
	AcceptFormat      string   `json:"accept_format"`
	AcceptDescription []string `json:"accept_description"`
	AcceptQuality     []int    `json:"accept_quality"`
	VideoCodecid      int      `json:"video_codecid"`
	VideoProject      bool     `json:"video_project"`
	SeekParam         string   `json:"seek_param"`
	SeekType          string   `json:"seek_type"`
	Durl              []struct {
		Order  int64  `json:"order"`
		Length int64  `json:"length"`
		Size   int64  `json:"size"`
		URL    string `json:"url"`
	} `json:"durl"`
}

func (this *BilibiliDownloader) getVideoInfoList_ByAidV2(aid int64) (resp GetVideoInfoList_Resp) {
	contents, err := this.defaultFetcher(fmt.Sprintf(_getCidUrlTemp, aid))
	if err != nil {
		resp.ErrMsg = "getVideoInfoList_ByAidV2 " + err.Error()
		return resp
	}
	var tmp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Title string `json:"title"`
			Pages []struct {
				Cid  int64  `json:"cid"`
				Page int64  `json:"page"`
				Vid  string `json:"vid"`
				Part string `json:"part"`
			} `json:"pages"`
		} `json:"data"`
	}
	err = json.Unmarshal(contents, &tmp)
	if err != nil {
		resp.ErrMsg = "getVideoInfoList_ByAidV2_2 " + err.Error()
		return resp
	}
	title := TitleEdit(tmp.Data.Title)
	FnMessage("获取视频名成功" + title)
	appKey, sec := GetAppKey(_entropy)
	var list []videoCid
	for _, i := range tmp.Data.Pages {
		cid := i.Cid
		page := i.Page
		part := TitleEdit(i.Part) //remove special characters

		cidStr := strconv.FormatInt(cid, 10)

		params := fmt.Sprintf(_paramsTemp, appKey, cidStr, _quality, _quality)
		chksum := fmt.Sprintf("%x", md5.Sum([]byte(params+sec)))

		urlApi := fmt.Sprintf(_playApiTemp, params, chksum)
		tmp2 := videoCid{
			Aid:    aid,
			Cid:    cid,
			Page:   page,
			Part:   part,
			UrlApi: urlApi,
		}
		contents, err = this.defaultFetcher(tmp2.UrlApi)
		if err != nil {
			resp.ErrMsg = err.Error()
			return resp
		}
		err = json.Unmarshal(contents, &tmp2.L1Data)
		if err != nil {
			resp.ErrMsg = err.Error()
			return resp
		}
		list = append(list, tmp2)
	}
	FnMessage("获取cidL1成功")
	title = TitleEdit(title)

	var list2 []DownloadVideoPart_Req
	var totalLength int64

	for _, one := range list {
		for _, two := range one.L1Data.Durl {
			list2 = append(list2, DownloadVideoPart_Req{
				Title:  title,
				Aid:    aid,
				Page:   one.Page,
				Cid:    one.Cid,
				Part:   TitleEdit(one.Part),
				Order:  two.Order,
				UrlApi: two.URL,
				Size:   two.Size,
			})
			totalLength += two.Size
		}
	}

	aidPath, err := this.GetAidFileDownloadDir(aid, title)
	if err != nil {
		resp.ErrMsg = err.Error()
		return resp
	}

	var listFlv []string
	var curLength int64
	for _, one := range list2 {
		var flvOne string
		err = this.DownloadVideoPart(one, aidPath, curLength, totalLength, &flvOne)
		if err != nil {
			resp.ErrMsg = err.Error()
			return resp
		}
		curLength += one.Size
		listFlv = append(listFlv, flvOne)
	}
	resp.OutMp4File = filepath.Join(this.req.SaveDir, strconv.FormatInt(aid, 10)+"_"+title+".mp4")
	err = MergeFlvFileListToSingleMp4(MergeTsFileListToSingleMp4_Req{
		FlvFileList:    listFlv,
		OutputMp4:      resp.OutMp4File,
		Ctx:            this.ctx,
		FlvTotalLength: totalLength,
	})
	if err != nil {
		resp.ErrMsg = err.Error()
		return resp
	}
	err = os.RemoveAll(aidPath)
	if err != nil {
		resp.ErrMsg = err.Error()
		return resp
	}
	return
}

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
	client := http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		req.Header.Set("Referer", referer)
		return nil
	}}
	defer client.CloseIdleConnections()

	request, err := http.NewRequest("GET", req.UrlApi, nil)
	if err != nil {
		return err
	}
	request = request.WithContext(this.ctx)

	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.13; rv:56.0) Gecko/20100101 Firefox/56.0")
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Accept-Language", "en-US,en;q=0.5")
	request.Header.Set("Accept-Encoding", "gzip, deflate, br")
	request.Header.Set("Range", "bytes=0-")
	request.Header.Set("Referer", referer)
	request.Header.Set("Origin", "https://www.bilibili.com")
	request.Header.Set("Connection", "keep-alive")

	resp, err := client.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("错误码： %d", resp.StatusCode)
	}

	filename := fmt.Sprintf("%d_%d.flv", req.Page, req.Order)
	*flvName = filepath.Join(aidPath, filename)
	file, err := os.Create(*flvName)
	if err != nil {
		return err
	}
	defer file.Close()

	r := &Reader{
		r: resp.Body,
	}
	var done int32
	go func() {
		for {
			this.sleepDur(time.Millisecond * 50)
			if atomic.LoadInt32(&done) == 1 || this.isCancel() {
				break
			}
			cur := r.N()
			FnUpdateProgress(float64(cur+curLength) / float64(totalLength))
		}
	}()
	err = func() error {
		_, err = io.Copy(file, r)
		if err != nil {
			return err
		}
		err = file.Sync()
		return err
	}()
	atomic.StoreInt32(&done, 1)
	if err != nil {
		log.Printf("下载失败 aid: %d, cid: %d, title: %s, part: %s", req.Aid, req.Cid, req.Title, req.Part)
		return err
	}

	return nil
}

func (this *BilibiliDownloader) defaultFetcher(url string) (content []byte, err error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("User-Agent", "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:60.0) Gecko/20100101 Firefox/60.0")
	request = request.WithContext(this.ctx)
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}

func (this *BilibiliDownloader) RunDownload() {
	defer func() {
		gRunningThreadCountLocker.Lock()
		defer gRunningThreadCountLocker.Unlock()

		gRunningThreadCount--
		if gRunningThreadCount == 0 {
			FnUpdateRunning(false)
		}
	}()
	FnMessage("开始解析视频信息")
	resp := this.GetVideoInfoListV2(this.req.Url)
	if this.isCancel() {
		return
	}
	if resp.ErrMsg != "" {
		FnError(resp.ErrMsg)
		return
	}
	FnDownloadFinish(resp.OutMp4File)
}

type GetVideoInfoList_Resp struct {
	ErrMsg     string
	OutMp4File string
}

func (this *BilibiliDownloader) isCancel() bool {
	select {
	case <-this.ctx.Done():
		return true
	default:
		return false
	}
}

func (this *BilibiliDownloader) sleepDur(duration time.Duration) {
	select {
	case <-time.After(duration):
	case <-this.ctx.Done():
	}
}

func (this *BilibiliDownloader) GetAidFileDownloadDir(aid int64, title string) (fullDirPath string, err error) {
	fullDirPath = filepath.Join(this.req.SaveDir, "download", fmt.Sprintf("%d_%s", aid, title))
	err = os.MkdirAll(fullDirPath, 0777)
	if err != nil {
		return "", err
	}
	return fullDirPath, nil
}

func GetAppKey(entropy string) (appkey, sec string) {
	revEntropy := ReverseRunes([]rune(entropy))
	for i := range revEntropy {
		revEntropy[i] = revEntropy[i] + 2
	}
	ret := strings.Split(string(revEntropy), ":")

	return ret[0], ret[1]
}

func ReverseRunes(runes []rune) []rune {
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return runes
}

func TitleEdit(title string) string { // will be used when save the title or the part
	// remove special symbol
	title = strings.Replace(title, ":", "", -1)
	title = strings.Replace(title, "\\", "", -1)
	title = strings.Replace(title, "/", "", -1)
	title = strings.Replace(title, "*", "", -1)
	title = strings.Replace(title, "?", "", -1)
	title = strings.Replace(title, "\"", "", -1)
	title = strings.Replace(title, "<", "", -1)
	title = strings.Replace(title, ">", "", -1)
	title = strings.Replace(title, "|", "", -1)
	title = strings.Replace(title, ".", "", -1)

	return title
}
