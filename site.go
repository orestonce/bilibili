package bilibili

import "net/http"

type VideoInfo struct {
	Name     string
	PartList []VideoPart
}

func (i VideoInfo) GetTotalLength() int64 {
	var total int64
	for _, one := range i.PartList {
		total += one.SizeValue
	}
	return total
}

type VideoPart struct {
	Name           string
	FileExtWithDot string
	DownloadUrl    string
	Header         http.Header
	HasSize        bool
	SizeValue      int64
}
