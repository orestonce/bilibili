package bilibili

import (
	"context"
	"errors"
	codec "github.com/yapingcat/gomedia/go-codec"
	flv "github.com/yapingcat/gomedia/go-flv"
	mp4 "github.com/yapingcat/gomedia/go-mp4"
	"io"
	"os"
	"strconv"
	"strings"
)

type MergeFileListToSingleMp4_Req struct {
	FileList       []string
	OutputMp4      string
	Ctx            context.Context
	FlvTotalLength int64
}

func MergeFileListToSingleMp4(req MergeFileListToSingleMp4_Req) (err error) {
	mp4file, err := os.OpenFile(req.OutputMp4, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	defer mp4file.Close()

	muxer, err := mp4.CreateMp4Muxer(mp4file)
	if err != nil {
		return err
	}
	vtid := muxer.AddVideoTrack(mp4.MP4_CODEC_H264)
	atid := muxer.AddAudioTrack(mp4.MP4_CODEC_AAC)

	var curLength int64

	for idx, partFile := range req.FileList {
		if strings.HasSuffix(partFile, ".mp4") {
			err = readMp4ToMuxer(req.Ctx, partFile, muxer, vtid, atid)
			if err != nil {
				return err
			}
			FnUpdateProgress(float64(idx) / float64(len(req.FileList)))
		} else if strings.HasSuffix(partFile, ".flv") {
			var OnFrameErr error
			demuxer := flv.CreateFlvReader()
			demuxer.OnFrame = func(cid codec.CodecID, frame []byte, pts uint32, dts uint32) {
				if OnFrameErr != nil {
					return
				}
				if cid == codec.CODECID_AUDIO_AAC {
					OnFrameErr = muxer.Write(atid, frame, uint64(pts), uint64(dts))
				} else if cid == codec.CODECID_VIDEO_H264 {
					OnFrameErr = muxer.Write(vtid, frame, uint64(pts), uint64(dts))
				} else {
					OnFrameErr = errors.New("unknown cid " + strconv.Itoa(int(cid)))
					return
				}
			}
			err = readFileToCb(partFile, func(buf []byte) error {
				err0 := demuxer.Input(buf)
				if err0 != nil {
					return err0
				}
				select {
				case <-req.Ctx.Done():
					return req.Ctx.Err()
				default:
				}
				curLength += int64(len(buf))
				FnUpdateProgress(float64(curLength) / float64(req.FlvTotalLength))
				return nil
			})
			if err != nil {
				return err
			}
			if OnFrameErr != nil {
				return OnFrameErr
			}
		} else {
			return errors.New("unsupported format " + partFile)
		}
	}

	err = muxer.WriteTrailer()
	if err != nil {
		return err
	}
	err = mp4file.Sync()
	if err != nil {
		return err
	}
	FnUpdateProgress(0)
	return nil
}

func readMp4ToMuxer(ctx context.Context, partFile string, muxer *mp4.Movmuxer, vtid uint32, atid uint32) (err error) {
	f, err := os.Open(partFile)
	if err != nil {
		return err
	}
	defer f.Close()

	mp4Demuxer := mp4.CreateMp4Demuxer(f)
	_, err = mp4Demuxer.ReadHead()
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var pkt *mp4.AVPacket
		pkt, err = mp4Demuxer.ReadPacket()
		if err != nil {
			if err != io.EOF {
				return err
			}
			return nil
		}
		if pkt.Cid == mp4.MP4_CODEC_AAC {
			err = muxer.Write(atid, pkt.Data, pkt.Pts, pkt.Dts)
		} else if pkt.Cid == mp4.MP4_CODEC_H264 {
			err = muxer.Write(vtid, pkt.Data, pkt.Pts, pkt.Dts)
		} else {
			err = errors.New("unknown cid " + strconv.Itoa(int(pkt.Cid)))
		}
		if err != nil {
			return err
		}
	}
}

func readFileToCb(fileName string, cb func(buf []byte) error) error {
	f, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer f.Close()

	var buf = make([]byte, 2*1024*1024) // 2MB
	for {
		var n int
		n, err = f.Read(buf)
		var isEof = false
		if err == io.EOF {
			isEof = true
		} else if err != nil {
			return err
		}
		err = cb(buf[:n])
		if err != nil {
			return err
		}
		if isEof {
			break
		}
	}
	return nil
}
