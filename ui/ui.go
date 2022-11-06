package main

import (
	"fmt"
	"github.com/gonutz/wui"
	"github.com/orestonce/bilibili"
	"os"
)

func main() {
	windowFont, _ := wui.NewFont(wui.FontDesc{
		Name:   "Tahoma",
		Height: -11,
	})

	window := wui.NewWindow()
	window.SetFont(windowFont)
	//window.SetInnerSize(664, 178)
	window.SetClientSize(664, 178)
	window.SetTitle("哔哩哔哩下载器")
	//window.SetResizable(false)

	label1 := wui.NewLabel()
	label1.SetBounds(20, 60, 65, 22)
	label1.SetText("下载目录")
	window.Add(label1)

	button_downloadDir := wui.NewButton()
	button_downloadDir.SetBounds(610, 60, 41, 22)
	button_downloadDir.SetText("...")
	window.Add(button_downloadDir)

	lineEdit_VideoUrl := wui.NewEditLine()
	//lineEdit_VideoUrl.SetHorizontalAnchor(wui.AnchorMax)
	lineEdit_VideoUrl.SetBounds(90, 30, 510, 22)
	//lineEdit_VideoUrl.SetCharacterLimit(2000)
	window.Add(lineEdit_VideoUrl)

	progressBar1 := wui.NewProgressBar()
	progressBar1.SetBounds(90, 90, 510, 22)
	window.Add(progressBar1)

	label2 := wui.NewLabel()
	label2.SetBounds(20, 90, 65, 22)
	label2.SetText("下载进度")
	window.Add(label2)

	label3 := wui.NewLabel()
	label3.SetBounds(20, 30, 65, 22)
	label3.SetText("视频URL")
	window.Add(label3)

	lineEdit_downloadDir := wui.NewEditLine()
	//lineEdit_downloadDir.SetVerticalAnchor(wui.AnchorCenter)
	lineEdit_downloadDir.SetBounds(90, 60, 510, 22)
	//lineEdit_downloadDir.SetCharacterLimit(2000)
	window.Add(lineEdit_downloadDir)

	button_startDownload := wui.NewButton()
	button_startDownload.SetBounds(410, 130, 80, 25)
	button_startDownload.SetText("开始下载")
	window.Add(button_startDownload)

	button_stopDownload := wui.NewButton()
	button_stopDownload.SetEnabled(false)
	button_stopDownload.SetBounds(510, 130, 80, 25)
	button_stopDownload.SetText("结束下载")
	window.Add(button_stopDownload)

	window.SetX(100)
	window.SetY(100)

	button_downloadDir.SetOnClick(func() {
		dlg := wui.NewFolderSelectDialog()
		dlg.SetTitle("下载目录")
		ok, dir := dlg.Execute(window)
		if ok == false {
			return
		}
		lineEdit_downloadDir.SetText(dir)
	})
	button_stopDownload.SetOnClick(func() {
		bilibili.StopDownload()
	})
	wd, _ := os.Getwd()
	lineEdit_downloadDir.SetText(wd)
	bilibili.InitPrintFnS(bilibili.PrintFnS{
		FnError: func(errMsg string) {
			wui.MessageBoxError(window, "错误", errMsg)
		},
		FnMessage: func(msg string) {
			fmt.Println("信息", msg)
		},
		FnUpdateProgress: func(d float64) {
			progressBar1.SetValue(d)
		},
		FnUpdateRunning: func(running bool) {
			button_startDownload.SetEnabled(running == false)
			button_stopDownload.SetEnabled(running)
			lineEdit_downloadDir.SetEnabled(running == false)
			lineEdit_VideoUrl.SetEnabled(running == false)
			button_downloadDir.SetEnabled(running == false)
			if running == false {
				progressBar1.SetValue(0)
			}
		},
		FnDownloadFinish: func(outMp4File string) {
			wui.MessageBox(window, "下载成功", outMp4File)
		},
	})
	button_startDownload.SetOnClick(func() {
		bilibili.BeginDownloadAsync(bilibili.BeginDownload_Req{
			Url:     lineEdit_VideoUrl.Text(),
			SaveDir: lineEdit_downloadDir.Text(),
		})
	})

	window.Show()
}
