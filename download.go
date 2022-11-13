package bilibili

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type BilibiliDownloader struct {
	ctx              context.Context
	closeFn          func()
	req              BeginDownload_Req
	speedBytesLocker sync.Mutex
	speedBeginTime   time.Time
	speedBytesMap    map[time.Time]int64
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
		req:           req,
		speedBytesMap: map[time.Time]int64{},
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
	} else if params = regexp.MustCompile(`https://www.douyin.com/video/(\d+)`).FindStringSubmatch(urlInput); len(params) > 0 {
		return this.getVideoListDouYin(params[1])
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
	FnMessage("视频名: " + title)
	appKey, sec := GetAppKey(_entropy)

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

	type videoCid struct {
		Aid    int64
		Cid    int64
		Page   int64
		Part   string
		UrlApi string
		L1Data cidL1
	}
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
	title = TitleEdit(title)

	var info VideoInfo
	var totalLength int64

	for _, one := range list {
		for _, two := range one.L1Data.Durl {
			header := make(http.Header)
			referer := fmt.Sprintf("https://api.bilibili.com/x/web-interface/view?aid=%d", aid)
			for i := 1; i <= int(one.Page); i++ {
				referer += fmt.Sprintf("&p=%d", i)
			}
			header.Set("User-Agent", userAgent)
			header.Set("Accept", "*/*")
			header.Set("Accept-Language", "en-US,en;q=0.5")
			header.Set("Accept-Encoding", "gzip, deflate, br")
			header.Set("Referer", referer)
			header.Set("Origin", "https://www.bilibili.com")
			header.Set("Connection", "keep-alive")

			info.PartList = append(info.PartList, VideoPart{
				Name:           fmt.Sprintf("%d_%d.%s", one.Page, two.Order, GetFormatForExt(one.L1Data.Format)),
				FileExtWithDot: "." + GetFormatForExt(one.L1Data.Format),
				DownloadUrl:    two.URL,
				Header:         header,
				HasSize:        true,
				SizeValue:      two.Size,
			})
			totalLength += two.Size
		}
	}
	if len(info.PartList) == 0 {
		resp.ErrMsg = "获取视频信息失败"
		return resp
	}

	info.Name = fmt.Sprintf("%d_%s", aid, title)

	return this.DownloadVideo(info)
}

func (this *BilibiliDownloader) DownloadVideo(info VideoInfo) (resp GetVideoInfoList_Resp) {
	if len(info.PartList) > 1 {
		err := os.MkdirAll(filepath.Join(this.req.SaveDir, info.Name), 0777)
		if err != nil {
			resp.ErrMsg = err.Error()
			return resp
		}
	}
	for idx, one := range info.PartList {
		if one.HasSize {
			continue
		}
		httpReq, err := http.NewRequest(http.MethodGet, one.DownloadUrl, nil)
		if err != nil {
			resp.ErrMsg = err.Error()
			return resp
		}
		httpResp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			resp.ErrMsg = err.Error()
			return resp
		}
		httpResp.Body.Close()
		i, err := strconv.ParseInt(httpResp.Header.Get("Content-Length"), 10, 64)
		if err != nil {
			resp.ErrMsg = err.Error()
			return resp
		}
		one.HasSize = true
		one.SizeValue = i
		info.PartList[idx] = one
	}
	totalLength := info.GetTotalLength()

	var curLength int64
	for _, one := range info.PartList {
		outName := filepath.Join(this.req.SaveDir, info.Name, one.Name)
		if len(info.PartList) == 1 {
			outName = filepath.Join(this.req.SaveDir, info.Name+one.FileExtWithDot)
		}
		err := this.DownloadVideoPart(one, outName, curLength, totalLength)
		if err != nil {
			resp.ErrMsg = err.Error()
			return resp
		}
		curLength += one.SizeValue
	}
	resp.OutName = info.Name
	return resp
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
	FnDownloadFinish(resp.OutName)
	FnMessage("")
}

type GetVideoInfoList_Resp struct {
	ErrMsg  string
	OutName string
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

func (this *BilibiliDownloader) getVideoListDouYin(vid string) (resp GetVideoInfoList_Resp) {
	content, err := this.defaultFetcher(`https://www.iesdouyin.com/web/api/v2/aweme/iteminfo/?item_ids=` + vid)
	if err != nil {
		resp.ErrMsg = err.Error()
		return resp
	}
	var tmp struct {
		ItemList []struct {
			Desc  string `json:"desc"`
			Video struct {
				PlayAddr struct {
					URI     string   `json:"uri"`
					URLList []string `json:"url_list"`
				} `json:"play_addr"`
				Vid string `json:"vid"`
			} `json:"video"`
		} `json:"item_list"`
		StatusCode int `json:"status_code"`
	}
	err = json.Unmarshal(content, &tmp)
	if err != nil {
		resp.ErrMsg = err.Error()
		return resp
	}
	var urlStr string
	if len(tmp.ItemList) > 0 && len(tmp.ItemList[0].Video.PlayAddr.URLList) > 0 {
		urlStr = tmp.ItemList[0].Video.PlayAddr.URLList[0]
		urlStr = strings.Replace(urlStr, "playwm", "play", 1)
	} else {
		resp.ErrMsg = "无法解析视频"
		return resp
	}
	title := TitleEdit(tmp.ItemList[0].Desc)

	info := VideoInfo{
		Name: title,
		PartList: []VideoPart{
			{
				Name:           title,
				FileExtWithDot: ".mp4",
				DownloadUrl:    urlStr,
				Header: map[string][]string{
					"User-Agent": {"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:60.0) Gecko/20100101 Firefox/60.0"},
				},
				HasSize: false,
			},
		},
	}
	return this.DownloadVideo(info)
}

const userAgent = "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:60.0) Gecko/20100101 Firefox/60.0"

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
	title = strings.Replace(title, "~", "", -1)
	title = strings.Replace(title, "\n", "", -1)

	return title
}
