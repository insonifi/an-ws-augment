package main

import (
	"context"
	"encoding/binary"
	"io/ioutil"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"time"
)

const (
	NOTIME       = 0
	EPOCH_OFFSET = -2208988800000
)

func GetData(
	req *http.Request,
	cmd WSCmd,
	wschan chan []byte,
	pending chan Signal,
) {
	client := &http.Client{}
	ctx, cancel := context.WithCancel(context.Background())
	reqctx := req.WithContext(ctx)
	log.Println("requesting", req.URL.RequestURI())
	res, err := client.Do(reqctx)
	if err != nil {
		log.Println(err)
		return
	}

	defer res.Body.Close()

	mediaType, params, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if err != nil {
		log.Println(err)
		return
	}

	prefix := append([]byte{0, byte(len(cmd.Endpoint))}, []byte(cmd.Endpoint)...)
	pkt_hdr := append(prefix /*, []byte(FCC[cmd.Format])...*/)

	var read StreamReader

	switch mediaType {
	case "multipart/x-mixed-replace":
		read = readMultiPart(&pkt_hdr, params["boundary"], res)
	case "image/jpeg":
		ts := getTime(req.Header.Get("X-Video-Original-Time"))
		pkt, err := readHttpWhole(&pkt_hdr, res, ts)
		if err != nil {
			log.Println(err)
			return
		}
		wschan <- pkt
		<-pending
		return
	case "video/mp4":
		read = readHttpStream(&pkt_hdr, res)
	}
	log.Println("pulling")
L:
	for {
		select {
		case <-pending:
			cancel()
			log.Println("STOPPED", cmd.StreamId)
			break L
		default:
			pkt, err := read()
			if err != nil {
				log.Println(err)
				break L
			}
			wschan <- pkt
		}
	}
}

/**
 * Stream Handlers
 */

func readHttpStream(hdr *[]byte, res *http.Response) StreamReader {
	buf := make([]byte, BUF_SIZE)

	log.Print("long-polling")
	return func() ([]byte, error) {
		n, err := res.Body.Read(buf)
		if err != nil {
			return nil, err
		}
		log.Println("read", n)
		pkt := makePacket(hdr, NOTIME, buf[:n])

		return pkt, nil
	}
}

func readMultiPart(
	hdr *[]byte,
	boundary string,
	res *http.Response,
) StreamReader {
	mp := multipart.NewReader(res.Body, boundary)

	log.Print("multipart")
	return func() ([]byte, error) {
		part, err := mp.NextPart()
		if err != nil {
			log.Println(err)
			return nil, err
		}

		frame, err := ioutil.ReadAll(part)
		if err != nil {
			log.Println(err)
			return nil, err
		}

		ts := getTime(part.Header.Get("X-Video-Original-Time"))

		pkt := makePacket(hdr, ts, frame)

		return pkt, nil
	}
}

func readHttpWhole(
	hdr *[]byte,
	res *http.Response,
	ts uint64,
) ([]byte, error) {
	log.Println("read whole")
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	pkt := makePacket(hdr, ts, body)

	return pkt, nil
}

func makePacket(
	pkt_hdr *[]byte,
	t uint64,
	payload []byte,
) []byte {
	tsBytes := make([]byte, 8)

	binary.BigEndian.PutUint64(tsBytes, uint64(t))
	pkt := append((*pkt_hdr), tsBytes...)
	pkt = append(pkt, payload...)

	return pkt
}

func getTime(strtime string) uint64 {
	if strtime != "" {
		ts, _ := time.Parse(
			ASIP_TIMEFORMAT,
			strtime,
		)
		js_time := ts.UnixNano() / time.Millisecond.Nanoseconds()
		js_time -= EPOCH_OFFSET

		return uint64(js_time)
	}
	return NOTIME
}
