package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gorilla/websocket"
)

type dataHandler func(net.Conn)
type WSCmd struct {
	Archive   string
	Method    string
	Endpoint  string
	BeginTime string
	Format    string
	StreamId	string
	Speed     int
	Width     int
	Height    int
	Q         int
}
type ReadableHeader interface {
	Get(name string) string
}
type Signal struct{}
type StreamReader func() ([]byte, error)

const (
	ARCHIVE         = "archive"
	ASIP_TIMEFORMAT = "20060102T150405.999999"
	BUF_SIZE        = 1 << 20 // 1K; << 16 // 64K
	LIVE            = "live"
	M_PLAY          = "play"
	M_STOP          = "stop"
)

var (
	backend  string
	port     int
	upgrader = websocket.Upgrader{}
	FCC      = map[string]string{
		"mjpeg": "mjpg",
		"mp4":   "fmp4",
	}
)

func video(w http.ResponseWriter, r *http.Request) {
	log.Println("WebSocket connection")
	wsc, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade: ", err)
		return
	}

	wschan := make(chan []byte)
	pending := make(map[string]chan Signal)

	go handleData(wsc, wschan, &pending)

	wsc.SetCloseHandler(func(code int, text string) error {
		cleanup(&pending)
		wsc.Close()
		log.Println("Closing WebSocket")

		return nil
	})

	for {
		var cmd WSCmd

		err := wsc.ReadJSON(&cmd)
		if err != nil {
			log.Println(err)
			break
		}
		handleWSCmd(wschan, &pending, cmd)
	}
}

func handleData(
	wsc *websocket.Conn,
	wschan chan []byte,
	pending *map[string]chan Signal,
) {
	for {
		select {
		case pkt := <-wschan:
			//log.Println("wsc:", "send", len(pkt))
			err := wsc.WriteMessage(websocket.BinaryMessage, pkt)
			if err != nil {
				log.Println("wsc:", err)
				cleanup(pending)

				return
			}
		}
	}
}

func cleanup(pending *map[string]chan Signal) {
	for endpoint, _ := range *pending {
		stopStream(pending, endpoint)
	}
}

func stopStream(pending *map[string]chan Signal, endpoint string) {
	if quit, ok := (*pending)[endpoint]; ok {
		log.Println("cleanup", endpoint)
		delete(*pending, endpoint)
		quit <- Signal{}
		close(quit)
		log.Println("clenup done")
	}
}

func makeRequest(url string, opts map[string]interface{}) (req *http.Request) {
	req, err := http.NewRequest("GET", url, bytes.NewReader(nil))

	if err != nil {
		log.Println(err)
		return
	}

	req.SetBasicAuth("root", "root")
	query := req.URL.Query()

	for key, value := range opts {
		query.Add(key, fmt.Sprint(value))
	}
	req.URL.RawQuery = query.Encode()

	return req
}

func handleWSCmd(
	wschan chan []byte,
	pending *map[string]chan Signal,
	cmd WSCmd,
) {
	log.Println(cmd)
	switch cmd.Method {
	case M_PLAY:
		opts := make(map[string]interface{})

		source := LIVE
		begintime := ""
		if cmd.BeginTime != "" {
			source = ARCHIVE
			begintime = fmt.Sprintf("/%s", cmd.BeginTime)
			opts["arhive"] = cmd.Archive
		}
		url := fmt.Sprintf(
			"%s/%s/media/%s%s",
			backend, source, cmd.Endpoint, begintime,
		)

		opts["format"] = cmd.Format

		if cmd.Speed > 0 {
			opts["speed"] = cmd.Speed
		}

		if cmd.Format == "jpeg" {
			opts["w"] = cmd.Width
			opts["h"] = cmd.Height
			opts["vc"] = cmd.Q
		}

		//stop pending streams
		stopStream(pending, cmd.StreamId)
		quit := make(chan Signal)

		(*pending)[cmd.StreamId] = quit

		req := makeRequest(url, opts)
		req.Header.Set("X-Video-Original-Time", cmd.BeginTime)

		go GetData(req, cmd, wschan, quit)
	case M_STOP:
		stopStream(pending, cmd.StreamId)

		return
	}
}

func main() {
	flag.StringVar(&backend, "backend", "http://localhost:8000", "Backend URI")
	flag.IntVar(&port, "listen", 9999, "Listening port")
	flag.Parse()
	mux := http.NewServeMux()
	bURL, err := url.Parse(backend)
	if err != nil {
		log.Fatal(err)
	}

	mux.HandleFunc("/ws", video)
	mux.Handle("/", httputil.NewSingleHostReverseProxy(bURL))

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), mux))
}
