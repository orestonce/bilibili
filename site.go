package bilibili

import "net/http"

type VideoInfo struct {
	Name     string
	PartList []VideoPart
}

type VideoPart struct {
	Name                   string
	FileExtWithDot         string
	DownloadUrl            string
	Header                 http.Header
	IsSupportRangeDownload bool
	SizeValue              int64
}
