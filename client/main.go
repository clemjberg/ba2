package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

type Position struct {
	GlobalSequence int64   `json:"globalSequence"`
	MessageID      string  `json:"messageId"`
	ClientID       int     `json:"clientId"`
	Sequence       int64   `json:"sequence"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`

	T0ClientGeneratedMs       int64 `json:"t0ClientGeneratedMs"`
	T1ServerReceivedMs        int64 `json:"t1ServerReceivedMs"`
	T2AvailableForDashboardMs int64 `json:"t2AvailableForDashboardMs"`

	Technology      string `json:"technology"`
	DataChannelMode string `json:"dataChannelMode"`
	DashboardMode   string `json:"dashboardMode"`
}

type WebRTCSignal struct {
	SDP string `json:"sdp"`
}

type ClockResponse struct {
	ServerTimeMs int64 `json:"serverTimeMs"`
}

var (
	technology      string
	dataChannelMode string
	serverURL       string
	clients         int
	durationString  string
	hz              int
	runID           string
	stagger         bool
	syncClock       bool

	sentMessagesTotal int64
	errorsTotal       int64

	clockOffsetMs int64

	clientMetricsFile string
	clientMetricsMu   sync.Mutex

	httpClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10000,
			MaxIdleConnsPerHost: 10000,
			IdleConnTimeout:     90 * time.Second,
		},
	}
)

func main() {
	flag.StringVar(&technology, "technology", "http-post", "Technology: http-post, websocket or webrtc")
	flag.StringVar(&dataChannelMode, "dcMode", "", "WebRTC DataChannel mode: reliable-ordered or unreliable-unordered")
	flag.StringVar(&serverURL, "server", "http://192.168.153.130:3000", "Server base URL")
	flag.IntVar(&clients, "clients", 1, "Number of simulated clients")
	flag.StringVar(&durationString, "duration", "5m", "Test duration, e.g. 30s, 5m")
	flag.IntVar(&hz, "hz", 1, "Update frequency per simulated client")
	flag.StringVar(&runID, "run", "run1", "Run identifier")
	flag.BoolVar(&stagger, "stagger", true, "Stagger client startup/sending")
	flag.BoolVar(&syncClock, "syncClock", true, "Estimate clock offset to server and timestamp messages in estimated server time")
	flag.Parse()

	if technology != "webrtc" {
		dataChannelMode = ""
	}

	if hz <= 0 {
		log.Fatal("hz must be greater than 0")
	}

	if clients <= 0 {
		log.Fatal("clients must be greater than 0")
	}

	duration, err := time.ParseDuration(durationString)
	if err != nil {
		log.Fatal("Invalid duration:", err)
	}

	err = os.MkdirAll("metrics", 0755)
	if err != nil {
		log.Fatal(err)
	}

	prepareClientMetricsFile()

	if syncClock {
		calibrateClock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Stopping client simulator...")
		cancel()
	}()

	go clientMetricsLogger(ctx)

	log.Println("===================================================")
	log.Println("BA2 Live Tracking Client Simulator")
	log.Println("Technology:       ", technology)
	log.Println("DataChannel Mode: ", dataChannelMode)
	log.Println("Server URL:       ", serverURL)
	log.Println("Clients:          ", clients)
	log.Println("Duration:         ", duration)
	log.Println("Hz:               ", hz)
	log.Println("Run:              ", runID)
	log.Println("Stagger:          ", stagger)
	log.Println("Sync Clock:       ", syncClock)
	log.Println("Clock Offset ms:  ", atomic.LoadInt64(&clockOffsetMs))
	log.Println("Metrics File:     ", clientMetricsFile)
	log.Println("===================================================")

	var wg sync.WaitGroup
	period := time.Second / time.Duration(hz)

	switch technology {
	case "http-post":
		for i := 1; i <= clients; i++ {
			wg.Add(1)
			go runHTTPPostClient(ctx, &wg, i, period)
		}
	case "websocket":
		for i := 1; i <= clients; i++ {
			wg.Add(1)
			go runWebSocketClient(ctx, &wg, i, period)
		}
	case "webrtc":
		for i := 1; i <= clients; i++ {
			wg.Add(1)
			go runWebRTCClient(ctx, &wg, i, period)
		}
	default:
		log.Fatal("Unknown technology. Use: http-post, websocket or webrtc")
	}

	wg.Wait()

	log.Println("===================================================")
	log.Println("Client test finished")
	log.Println("Sent messages: ", atomic.LoadInt64(&sentMessagesTotal))
	log.Println("Errors:        ", atomic.LoadInt64(&errorsTotal))
	log.Println("Metrics File:  ", clientMetricsFile)
	log.Println("===================================================")
}

func testLabel() string {
	if technology == "http-post" {
		return "http-post-long-polling"
	}

	if technology == "webrtc" && dataChannelMode != "" {
		return "webrtc-" + dataChannelMode
	}

	return technology
}

func prepareClientMetricsFile() {
	label := testLabel()

	clientMetricsFile = fmt.Sprintf(
		"metrics/client_load_%s_%dclients_%dhz_%s.csv",
		label,
		clients,
		hz,
		runID,
	)

	file, err := os.Create(clientMetricsFile)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	err = writer.Write([]string{
		"timestamp_ms",
		"technology",
		"data_channel_mode",
		"scenario_clients",
		"hz",
		"run_id",
		"sent_messages_total",
		"sent_messages_per_second",
		"errors_total",
		"errors_per_second",
		"clock_offset_ms",
	})
	if err != nil {
		log.Fatal(err)
	}
}

func clientMetricsLogger(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var previousSent int64
	var previousErrors int64

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentSent := atomic.LoadInt64(&sentMessagesTotal)
			currentErrors := atomic.LoadInt64(&errorsTotal)

			sentPerSecond := currentSent - previousSent
			errorsPerSecond := currentErrors - previousErrors

			previousSent = currentSent
			previousErrors = currentErrors

			writeClientMetricsRow(currentSent, sentPerSecond, currentErrors, errorsPerSecond)
		}
	}
}

func writeClientMetricsRow(currentSent int64, sentPerSecond int64, currentErrors int64, errorsPerSecond int64) {
	clientMetricsMu.Lock()
	defer clientMetricsMu.Unlock()

	file, err := os.OpenFile(clientMetricsFile, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Println("Could not open client metrics file:", err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	row := []string{
		strconv.FormatInt(nowLocalMs(), 10),
		technology,
		dataChannelMode,
		strconv.Itoa(clients),
		strconv.Itoa(hz),
		runID,
		strconv.FormatInt(currentSent, 10),
		strconv.FormatInt(sentPerSecond, 10),
		strconv.FormatInt(currentErrors, 10),
		strconv.FormatInt(errorsPerSecond, 10),
		strconv.FormatInt(atomic.LoadInt64(&clockOffsetMs), 10),
	}

	writer.Write(row)
}

func nowLocalMs() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func nowServerEstimatedMs() int64 {
	return nowLocalMs() + atomic.LoadInt64(&clockOffsetMs)
}

func calibrateClock() {
	type sample struct {
		offset int64
		rtt    int64
	}

	samples := make([]sample, 0)

	for i := 0; i < 7; i++ {
		tStart := nowLocalMs()

		resp, err := httpClient.Get(strings.TrimRight(serverURL, "/") + "/api/clock")
		if err != nil {
			log.Println("Clock calibration failed:", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Println("Clock calibration read failed:", err)
			continue
		}

		tEnd := nowLocalMs()

		var clockResponse ClockResponse
		err = json.Unmarshal(body, &clockResponse)
		if err != nil {
			log.Println("Clock calibration JSON failed:", err)
			continue
		}

		rtt := tEnd - tStart
		midpoint := (tStart + tEnd) / 2
		offset := clockResponse.ServerTimeMs - midpoint

		samples = append(samples, sample{
			offset: offset,
			rtt:    rtt,
		})

		time.Sleep(100 * time.Millisecond)
	}

	if len(samples) == 0 {
		log.Println("Clock calibration failed completely. Using offset 0.")
		atomic.StoreInt64(&clockOffsetMs, 0)
		return
	}

	best := samples[0]
	for _, s := range samples {
		if s.rtt < best.rtt {
			best = s
		}
	}

	atomic.StoreInt64(&clockOffsetMs, best.offset)

	log.Println("Clock calibration done. Best RTT ms:", best.rtt, "Offset ms:", best.offset)
}

func runHTTPPostClient(ctx context.Context, wg *sync.WaitGroup, clientID int, period time.Duration) {
	defer wg.Done()

	applyStaggerDelay(ctx, clientID, period)

	ticker := time.NewTicker(period)
	defer ticker.Stop()

	var sequence int64

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sequence++
			position := generatePosition(clientID, sequence)

			err := sendHTTPPosition(position)
			if err != nil {
				atomic.AddInt64(&errorsTotal, 1)
				continue
			}

			atomic.AddInt64(&sentMessagesTotal, 1)
		}
	}
}

func sendHTTPPosition(position Position) error {
	payload, err := json.Marshal(position)
	if err != nil {
		return err
	}

	url := strings.TrimRight(serverURL, "/") + "/api/location"

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}

	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	return nil
}

func runWebSocketClient(ctx context.Context, wg *sync.WaitGroup, clientID int, period time.Duration) {
	defer wg.Done()

	applyStaggerDelay(ctx, clientID, period)

	wsURL := serverURLToWebSocketURL(serverURL, "/ws/ingest")

	dialer := websocket.Dialer{
		HandshakeTimeout:  10 * time.Second,
		EnableCompression: false,
	}

	conn, _, err := dialer.Dial(wsURL, nil)

	if err != nil {
		log.Println("WebSocket dial error client", clientID, ":", err)
		atomic.AddInt64(&errorsTotal, 1)
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(period)
	defer ticker.Stop()

	var sequence int64

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sequence++
			position := generatePosition(clientID, sequence)

			err := conn.WriteJSON(position)
			if err != nil {
				atomic.AddInt64(&errorsTotal, 1)
				return
			}

			atomic.AddInt64(&sentMessagesTotal, 1)
		}
	}
}

func serverURLToWebSocketURL(baseURL string, path string) string {
	url := strings.TrimRight(baseURL, "/")

	if strings.HasPrefix(url, "https://") {
		url = "wss://" + strings.TrimPrefix(url, "https://")
	} else if strings.HasPrefix(url, "http://") {
		url = "ws://" + strings.TrimPrefix(url, "http://")
	}

	return url + path
}

func runWebRTCClient(ctx context.Context, wg *sync.WaitGroup, clientID int, period time.Duration) {
	defer wg.Done()

	applyStaggerDelay(ctx, clientID, period)

	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		log.Println("WebRTC peer connection error client", clientID, ":", err)
		atomic.AddInt64(&errorsTotal, 1)
		return
	}
	defer peerConnection.Close()

	dataChannel, err := peerConnection.CreateDataChannel("locations", getDataChannelInit())
	if err != nil {
		log.Println("WebRTC datachannel error client", clientID, ":", err)
		atomic.AddInt64(&errorsTotal, 1)
		return
	}

	openCh := make(chan struct{})
	closeOnce := sync.Once{}

	dataChannel.OnOpen(func() {
		closeOnce.Do(func() {
			close(openCh)
		})
	})

	dataChannel.OnError(func(err error) {
		log.Println("WebRTC datachannel runtime error client", clientID, ":", err)
	})

	offer, err := peerConnection.CreateOffer(nil)
	if err != nil {
		log.Println("WebRTC create offer error client", clientID, ":", err)
		atomic.AddInt64(&errorsTotal, 1)
		return
	}

	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	err = peerConnection.SetLocalDescription(offer)
	if err != nil {
		log.Println("WebRTC set local description error client", clientID, ":", err)
		atomic.AddInt64(&errorsTotal, 1)
		return
	}

	<-gatherComplete

	localDescription := peerConnection.LocalDescription()
	offerBytes, err := json.Marshal(localDescription)
	if err != nil {
		log.Println("WebRTC marshal offer error client", clientID, ":", err)
		atomic.AddInt64(&errorsTotal, 1)
		return
	}

	answerSignal, err := sendWebRTCOffer("/webrtc/ingest/offer", WebRTCSignal{
		SDP: base64.StdEncoding.EncodeToString(offerBytes),
	})
	if err != nil {
		log.Println("WebRTC signaling error client", clientID, ":", err)
		atomic.AddInt64(&errorsTotal, 1)
		return
	}

	answerBytes, err := base64.StdEncoding.DecodeString(answerSignal.SDP)
	if err != nil {
		log.Println("WebRTC decode answer error client", clientID, ":", err)
		atomic.AddInt64(&errorsTotal, 1)
		return
	}

	var answer webrtc.SessionDescription
	err = json.Unmarshal(answerBytes, &answer)
	if err != nil {
		log.Println("WebRTC unmarshal answer error client", clientID, ":", err)
		atomic.AddInt64(&errorsTotal, 1)
		return
	}

	err = peerConnection.SetRemoteDescription(answer)
	if err != nil {
		log.Println("WebRTC set remote description error client", clientID, ":", err)
		atomic.AddInt64(&errorsTotal, 1)
		return
	}

	select {
	case <-openCh:
	case <-time.After(10 * time.Second):
		log.Println("WebRTC datachannel open timeout client", clientID)
		atomic.AddInt64(&errorsTotal, 1)
		return
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(period)
	defer ticker.Stop()

	var sequence int64

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sequence++
			position := generatePosition(clientID, sequence)

			payload, err := json.Marshal(position)
			if err != nil {
				atomic.AddInt64(&errorsTotal, 1)
				continue
			}

			err = dataChannel.SendText(string(payload))
			if err != nil {
				atomic.AddInt64(&errorsTotal, 1)
				return
			}

			atomic.AddInt64(&sentMessagesTotal, 1)
		}
	}
}

func getDataChannelInit() *webrtc.DataChannelInit {
	if dataChannelMode == "" || dataChannelMode == "reliable-ordered" {
		return nil
	}

	if dataChannelMode == "unreliable-unordered" {
		ordered := false
		maxRetransmits := uint16(0)

		return &webrtc.DataChannelInit{
			Ordered:        &ordered,
			MaxRetransmits: &maxRetransmits,
		}
	}

	log.Fatal("Unknown dcMode. Use: reliable-ordered or unreliable-unordered")
	return nil
}

func sendWebRTCOffer(path string, signal WebRTCSignal) (WebRTCSignal, error) {
	payload, err := json.Marshal(signal)
	if err != nil {
		return WebRTCSignal{}, err
	}

	url := strings.TrimRight(serverURL, "/") + path

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return WebRTCSignal{}, err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return WebRTCSignal{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return WebRTCSignal{}, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return WebRTCSignal{}, fmt.Errorf("signaling status %d: %s", resp.StatusCode, string(body))
	}

	var answer WebRTCSignal
	err = json.Unmarshal(body, &answer)
	if err != nil {
		return WebRTCSignal{}, err
	}

	return answer, nil
}

func applyStaggerDelay(ctx context.Context, clientID int, period time.Duration) {
	if !stagger || clients <= 1 {
		return
	}

	step := period / time.Duration(clients)
	if step <= 0 {
		step = time.Millisecond
	}

	delay := time.Duration(clientID-1) * step

	select {
	case <-ctx.Done():
		return
	case <-time.After(delay):
		return
	}
}

func generatePosition(clientID int, sequence int64) Position {
	baseLat := 48.2082
	baseLon := 16.3738

	clientOffset := float64(clientID) * 0.00001
	movement := float64(sequence) * 0.000005

	lat := baseLat + clientOffset + math.Sin(float64(sequence)/10.0)*movement
	lon := baseLon + clientOffset + math.Cos(float64(sequence)/10.0)*movement

	return Position{
		MessageID:                 fmt.Sprintf("%d-%d", clientID, sequence),
		ClientID:                  clientID,
		Sequence:                  sequence,
		Latitude:                  lat,
		Longitude:                 lon,
		T0ClientGeneratedMs:       nowServerEstimatedMs(),
		Technology:                technology,
		DataChannelMode:           dataChannelMode,
		DashboardMode:             "",
		GlobalSequence:            0,
		T1ServerReceivedMs:        0,
		T2AvailableForDashboardMs: 0,
	}
}
