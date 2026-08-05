package main

import (
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
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

type IngestionRawRow struct {
	Position
	LatencyMs int64 `json:"latencyMs"`
	JitterMs  int64 `json:"jitterMs"`
}

type VisualizationAck struct {
	Technology      string `json:"technology"`
	DataChannelMode string `json:"dataChannelMode"`
	DashboardMode   string `json:"dashboardMode"`
	ScenarioClients int    `json:"scenarioClients"`
	Hz              int    `json:"hz"`
	RunID           string `json:"runId"`

	GlobalSequence int64  `json:"globalSequence"`
	MessageID      string `json:"messageId"`
	ClientID       int    `json:"clientId"`
	Sequence       int64  `json:"sequence"`

	T0ClientGeneratedMs       int64 `json:"t0ClientGeneratedMs"`
	T1ServerReceivedMs        int64 `json:"t1ServerReceivedMs"`
	T2AvailableForDashboardMs int64 `json:"t2AvailableForDashboardMs"`
	T3BrowserReceivedMs       int64 `json:"t3BrowserReceivedMs"`
	T4DomUpdatedMs            int64 `json:"t4DomUpdatedMs"`
	T5PaintCompletedMs        int64 `json:"t5PaintCompletedMs"`
}

type StatusResponse struct {
	Technology                 string     `json:"technology"`
	DataChannelMode            string     `json:"dataChannelMode"`
	DashboardMode              string     `json:"dashboardMode"`
	ScenarioClients            int        `json:"scenarioClients"`
	Hz                         int        `json:"hz"`
	RunID                      string     `json:"runId"`
	ReceivedMessagesTotal      int64      `json:"receivedMessagesTotal"`
	ActiveIngestConnections    int64      `json:"activeIngestConnections"`
	ActiveDashboardConnections int64      `json:"activeDashboardConnections"`
	LatestPositions            []Position `json:"latestPositions"`
}

type DashboardUpdatesResponse struct {
	Technology      string     `json:"technology"`
	DataChannelMode string     `json:"dataChannelMode"`
	DashboardMode   string     `json:"dashboardMode"`
	Updates         []Position `json:"updates"`
}

type WebRTCSignal struct {
	SDP string `json:"sdp"`
}

type DashboardWSClient struct {
	Conn *websocket.Conn
	Mu   sync.Mutex
}

type ServerState struct {
	mu sync.Mutex

	latestPositions     map[int]Position
	history             []Position
	lastLatencyByClient map[int]int64

	dashboardWSConnections  map[*DashboardWSClient]bool
	dashboardRTCChannels    map[*webrtc.DataChannel]bool
	ingestPeerConnections   map[*webrtc.PeerConnection]bool
	dashboardPeerConnection map[*webrtc.PeerConnection]bool
}

var (
	technology      string
	dataChannelMode string
	dashboardMode   string
	scenarioClients int
	hz              int
	runID           string
	port            int

	state ServerState

	globalSequence int64

	receivedMessagesTotal int64
	activeIngestWS        int64
	activeIngestRTC       int64
	activeDashboardWS     int64
	activeDashboardRTC    int64

	ingestionFile     string
	visualizationFile string
	resourcesFile     string

	ingestionMu     sync.Mutex
	visualizationMu sync.Mutex
	resourcesMu     sync.Mutex

	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
)

func main() {
	flag.StringVar(&technology, "technology", "http-post", "Technology: http-post, websocket, webrtc")
	flag.StringVar(&dataChannelMode, "dcMode", "", "WebRTC DataChannel mode: reliable-ordered or unreliable-unordered")
	flag.StringVar(&dashboardMode, "dashboardMode", "long-polling", "Dashboard mode: long-polling, websocket, webrtc")
	flag.IntVar(&scenarioClients, "clients", 1, "Number of simulated clients")
	flag.IntVar(&hz, "hz", 1, "Update frequency per client")
	flag.StringVar(&runID, "run", "run1", "Run identifier")
	flag.IntVar(&port, "port", 3000, "HTTP server port")
	flag.Parse()

	if technology != "webrtc" {
		dataChannelMode = ""
	}

	initState()
	initMetricsFiles()

	go resourceLogger()

	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/dashboard", dashboardHandler)
	http.HandleFunc("/api/clock", clockHandler)
	http.HandleFunc("/api/status", statusHandler)
	http.HandleFunc("/api/location", httpPostLocationHandler)
	http.HandleFunc("/api/visualization-ack", visualizationAckHandler)

	http.HandleFunc("/ws/ingest", ingestWebSocketHandler)
	http.HandleFunc("/webrtc/ingest/offer", ingestWebRTCOfferHandler)

	http.HandleFunc("/dashboard/long-poll", dashboardLongPollHandler)
	http.HandleFunc("/dashboard/ws", dashboardWebSocketHandler)
	http.HandleFunc("/dashboard/webrtc/offer", dashboardWebRTCOfferHandler)

	addr := fmt.Sprintf(":%d", port)

	log.Println("===================================================")
	log.Println("BA2 Live Tracking Server")
	log.Println("Technology:       ", technology)
	log.Println("DataChannel Mode: ", dataChannelMode)
	log.Println("Dashboard Mode:   ", dashboardMode)
	log.Println("Clients:          ", scenarioClients)
	log.Println("Hz:               ", hz)
	log.Println("Run:              ", runID)
	log.Println("Port:             ", port)
	log.Println("Dashboard:        ", "http://192.168.153.130:"+strconv.Itoa(port)+"/dashboard")
	log.Println("HTTP Ingest:      ", "POST /api/location")
	log.Println("WebSocket Ingest: ", "GET  /ws/ingest")
	log.Println("WebRTC Ingest:    ", "POST /webrtc/ingest/offer")
	log.Println("Long Poll Dash:   ", "GET  /dashboard/long-poll")
	log.Println("WebSocket Dash:   ", "GET  /dashboard/ws")
	log.Println("WebRTC Dash:      ", "POST /dashboard/webrtc/offer")
	log.Println("===================================================")

	err := http.ListenAndServe(addr, nil)
	if err != nil {
		log.Fatal(err)
	}
}

func initState() {
	state = ServerState{
		latestPositions:         make(map[int]Position),
		history:                 make([]Position, 0, 1024),
		lastLatencyByClient:     make(map[int]int64),
		dashboardWSConnections:  make(map[*DashboardWSClient]bool),
		dashboardRTCChannels:    make(map[*webrtc.DataChannel]bool),
		ingestPeerConnections:   make(map[*webrtc.PeerConnection]bool),
		dashboardPeerConnection: make(map[*webrtc.PeerConnection]bool),
	}
}

func testLabel() string {
	if technology == "http-post" && dashboardMode == "long-polling" {
		return "http-post-long-polling"
	}

	if technology == "webrtc" && dataChannelMode != "" {
		return "webrtc-" + dataChannelMode
	}

	return technology
}

func initMetricsFiles() {
	err := os.MkdirAll("metrics", 0755)
	if err != nil {
		log.Fatal(err)
	}

	label := testLabel()

	ingestionFile = fmt.Sprintf(
		"metrics/raw_ingestion_%s_%dclients_%dhz_%s.csv",
		label,
		scenarioClients,
		hz,
		runID,
	)

	visualizationFile = fmt.Sprintf(
		"metrics/raw_visualization_%s_%dclients_%dhz_%s.csv",
		label,
		scenarioClients,
		hz,
		runID,
	)

	resourcesFile = fmt.Sprintf(
		"metrics/resources_server_process_%s_%dclients_%dhz_%s.csv",
		label,
		scenarioClients,
		hz,
		runID,
	)

	writeCSVHeader(ingestionFile, []string{
		"technology",
		"data_channel_mode",
		"dashboard_mode",
		"scenario_clients",
		"hz",
		"run_id",
		"global_sequence",
		"message_id",
		"client_id",
		"sequence",
		"t0_client_generated_ms",
		"t1_server_received_ms",
		"t2_available_dashboard_ms",
		"latency_ms",
		"jitter_ms",
	})

	writeCSVHeader(visualizationFile, []string{
		"technology",
		"data_channel_mode",
		"dashboard_mode",
		"scenario_clients",
		"hz",
		"run_id",
		"global_sequence",
		"message_id",
		"client_id",
		"sequence",
		"t0_client_generated_ms",
		"t1_server_received_ms",
		"t2_available_dashboard_ms",
		"t3_browser_received_ms",
		"t4_dom_updated_ms",
		"t5_paint_completed_ms",
		"network_ingestion_ms",
		"server_processing_ms",
		"dashboard_delivery_ms",
		"dom_update_ms",
		"paint_delay_ms",
		"end_to_end_visualization_ms",
	})

	writeCSVHeader(resourcesFile, []string{
		"timestamp_ms",
		"technology",
		"data_channel_mode",
		"dashboard_mode",
		"scenario_clients",
		"hz",
		"run_id",
		"cpu_percent_process",
		"ram_mb_process",
		"go_goroutines",
		"active_ingest_connections",
		"active_dashboard_connections",
		"received_messages_total",
	})
}

func writeCSVHeader(filename string, header []string) {
	file, err := os.Create(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	err = writer.Write(header)
	if err != nil {
		log.Fatal(err)
	}
}

func nowMs() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func clockHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]int64{
		"serverTimeMs": nowMs(),
	})
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	state.mu.Lock()

	positions := make([]Position, 0, len(state.latestPositions))
	for _, pos := range state.latestPositions {
		positions = append(positions, pos)
	}

	state.mu.Unlock()

	writeJSON(w, StatusResponse{
		Technology:                 technology,
		DataChannelMode:            dataChannelMode,
		DashboardMode:              dashboardMode,
		ScenarioClients:            scenarioClients,
		Hz:                         hz,
		RunID:                      runID,
		ReceivedMessagesTotal:      atomic.LoadInt64(&receivedMessagesTotal),
		ActiveIngestConnections:    atomic.LoadInt64(&activeIngestWS) + atomic.LoadInt64(&activeIngestRTC),
		ActiveDashboardConnections: atomic.LoadInt64(&activeDashboardWS) + atomic.LoadInt64(&activeDashboardRTC),
		LatestPositions:            positions,
	})
}

func httpPostLocationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	var position Position
	err := json.NewDecoder(r.Body).Decode(&position)
	if err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	processPosition(position)

	writeJSON(w, map[string]string{
		"status": "ok",
	})
}

func ingestWebSocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	atomic.AddInt64(&activeIngestWS, 1)
	defer atomic.AddInt64(&activeIngestWS, -1)
	defer conn.Close()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var position Position
		err = json.Unmarshal(message, &position)
		if err != nil {
			continue
		}

		processPosition(position)
	}
}

func ingestWebRTCOfferHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	var signal WebRTCSignal
	err := json.NewDecoder(r.Body).Decode(&signal)
	if err != nil {
		http.Error(w, "Invalid signal JSON", http.StatusBadRequest)
		return
	}

	offerBytes, err := base64.StdEncoding.DecodeString(signal.SDP)
	if err != nil {
		http.Error(w, "Invalid base64 SDP", http.StatusBadRequest)
		return
	}

	var offer webrtc.SessionDescription
	err = json.Unmarshal(offerBytes, &offer)
	if err != nil {
		http.Error(w, "Invalid SDP JSON", http.StatusBadRequest)
		return
	}

	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	state.mu.Lock()
	state.ingestPeerConnections[peerConnection] = true
	state.mu.Unlock()

	atomic.AddInt64(&activeIngestRTC, 1)

	peerConnection.OnConnectionStateChange(func(connectionState webrtc.PeerConnectionState) {
		if connectionState == webrtc.PeerConnectionStateFailed ||
			connectionState == webrtc.PeerConnectionStateClosed ||
			connectionState == webrtc.PeerConnectionStateDisconnected {
			state.mu.Lock()
			delete(state.ingestPeerConnections, peerConnection)
			state.mu.Unlock()
			atomic.AddInt64(&activeIngestRTC, -1)
			peerConnection.Close()
		}
	})

	peerConnection.OnDataChannel(func(dataChannel *webrtc.DataChannel) {
		dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
			var position Position
			err := json.Unmarshal(msg.Data, &position)
			if err != nil {
				return
			}

			processPosition(position)
		})
	})

	err = peerConnection.SetRemoteDescription(offer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	err = peerConnection.SetLocalDescription(answer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	<-gatherComplete

	localDescription := peerConnection.LocalDescription()
	answerBytes, err := json.Marshal(localDescription)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, WebRTCSignal{
		SDP: base64.StdEncoding.EncodeToString(answerBytes),
	})
}

func processPosition(position Position) {
	t1 := nowMs()

	if position.MessageID == "" {
		position.MessageID = fmt.Sprintf("%d-%d", position.ClientID, position.Sequence)
	}

	position.Technology = technology
	position.DataChannelMode = dataChannelMode
	position.DashboardMode = dashboardMode
	position.T1ServerReceivedMs = t1

	globalSeq := atomic.AddInt64(&globalSequence, 1)
	position.GlobalSequence = globalSeq

	var latency int64
	var jitter int64

	state.mu.Lock()

	position.T2AvailableForDashboardMs = nowMs()

	latency = position.T1ServerReceivedMs - position.T0ClientGeneratedMs

	previousLatency, exists := state.lastLatencyByClient[position.ClientID]
	if exists {
		jitter = absInt64(latency - previousLatency)
	} else {
		jitter = 0
	}

	state.lastLatencyByClient[position.ClientID] = latency
	state.latestPositions[position.ClientID] = position
	state.history = append(state.history, position)

	wsTargets := make([]*DashboardWSClient, 0, len(state.dashboardWSConnections))
	for client := range state.dashboardWSConnections {
		wsTargets = append(wsTargets, client)
	}

	rtcTargets := make([]*webrtc.DataChannel, 0, len(state.dashboardRTCChannels))
	for dc := range state.dashboardRTCChannels {
		rtcTargets = append(rtcTargets, dc)
	}

	state.mu.Unlock()

	atomic.AddInt64(&receivedMessagesTotal, 1)

	writeIngestionRaw(position, latency, jitter)
	pushToDashboardTargets(position, wsTargets, rtcTargets)
}

func pushToDashboardTargets(position Position, wsTargets []*DashboardWSClient, rtcTargets []*webrtc.DataChannel) {
	for _, client := range wsTargets {
		client.Mu.Lock()
		err := client.Conn.WriteJSON(position)
		client.Mu.Unlock()

		if err != nil {
			state.mu.Lock()
			delete(state.dashboardWSConnections, client)
			state.mu.Unlock()
			client.Conn.Close()
		}
	}

	payload, err := json.Marshal(position)
	if err != nil {
		return
	}

	for _, dc := range rtcTargets {
		if dc.ReadyState() == webrtc.DataChannelStateOpen {
			err := dc.SendText(string(payload))
			if err != nil {
				state.mu.Lock()
				delete(state.dashboardRTCChannels, dc)
				state.mu.Unlock()
			}
		}
	}
}

func writeIngestionRaw(position Position, latency int64, jitter int64) {
	ingestionMu.Lock()
	defer ingestionMu.Unlock()

	file, err := os.OpenFile(ingestionFile, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Println("Could not open ingestion file:", err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	row := []string{
		position.Technology,
		position.DataChannelMode,
		position.DashboardMode,
		strconv.Itoa(scenarioClients),
		strconv.Itoa(hz),
		runID,
		strconv.FormatInt(position.GlobalSequence, 10),
		position.MessageID,
		strconv.Itoa(position.ClientID),
		strconv.FormatInt(position.Sequence, 10),
		strconv.FormatInt(position.T0ClientGeneratedMs, 10),
		strconv.FormatInt(position.T1ServerReceivedMs, 10),
		strconv.FormatInt(position.T2AvailableForDashboardMs, 10),
		strconv.FormatInt(latency, 10),
		strconv.FormatInt(jitter, 10),
	}

	writer.Write(row)
}

func dashboardLongPollHandler(w http.ResponseWriter, r *http.Request) {
	lastSeen := int64(0)

	rawLastSeen := r.URL.Query().Get("lastSeenGlobalSequence")
	if rawLastSeen != "" {
		parsed, err := strconv.ParseInt(rawLastSeen, 10, 64)
		if err == nil {
			lastSeen = parsed
		}
	}

	deadline := time.Now().Add(25 * time.Second)

	for {
		updates := getUpdatesAfter(lastSeen)
		if len(updates) > 0 {
			writeJSON(w, DashboardUpdatesResponse{
				Technology:      technology,
				DataChannelMode: dataChannelMode,
				DashboardMode:   dashboardMode,
				Updates:         updates,
			})
			return
		}

		if time.Now().After(deadline) {
			writeJSON(w, DashboardUpdatesResponse{
				Technology:      technology,
				DataChannelMode: dataChannelMode,
				DashboardMode:   dashboardMode,
				Updates:         []Position{},
			})
			return
		}

		time.Sleep(25 * time.Millisecond)
	}
}

func getUpdatesAfter(lastSeen int64) []Position {
	state.mu.Lock()
	defer state.mu.Unlock()

	updates := make([]Position, 0)

	for _, position := range state.history {
		if position.GlobalSequence > lastSeen {
			updates = append(updates, position)
		}
	}

	if len(updates) > 1000 {
		return updates[len(updates)-1000:]
	}

	return updates
}

func dashboardWebSocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := &DashboardWSClient{
		Conn: conn,
	}

	atomic.AddInt64(&activeDashboardWS, 1)

	state.mu.Lock()
	state.dashboardWSConnections[client] = true
	state.mu.Unlock()

	defer func() {
		state.mu.Lock()
		delete(state.dashboardWSConnections, client)
		state.mu.Unlock()

		atomic.AddInt64(&activeDashboardWS, -1)
		conn.Close()
	}()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			return
		}
	}
}

func dashboardWebRTCOfferHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	var signal WebRTCSignal
	err := json.NewDecoder(r.Body).Decode(&signal)
	if err != nil {
		http.Error(w, "Invalid signal JSON", http.StatusBadRequest)
		return
	}

	offerBytes, err := base64.StdEncoding.DecodeString(signal.SDP)
	if err != nil {
		http.Error(w, "Invalid base64 SDP", http.StatusBadRequest)
		return
	}

	var offer webrtc.SessionDescription
	err = json.Unmarshal(offerBytes, &offer)
	if err != nil {
		http.Error(w, "Invalid SDP JSON", http.StatusBadRequest)
		return
	}

	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	state.mu.Lock()
	state.dashboardPeerConnection[peerConnection] = true
	state.mu.Unlock()

	peerConnection.OnConnectionStateChange(func(connectionState webrtc.PeerConnectionState) {
		if connectionState == webrtc.PeerConnectionStateFailed ||
			connectionState == webrtc.PeerConnectionStateClosed ||
			connectionState == webrtc.PeerConnectionStateDisconnected {
			state.mu.Lock()
			delete(state.dashboardPeerConnection, peerConnection)
			state.mu.Unlock()
			peerConnection.Close()
		}
	})

	peerConnection.OnDataChannel(func(dataChannel *webrtc.DataChannel) {
		dataChannel.OnOpen(func() {
			state.mu.Lock()
			state.dashboardRTCChannels[dataChannel] = true
			state.mu.Unlock()

			atomic.AddInt64(&activeDashboardRTC, 1)
		})

		dataChannel.OnClose(func() {
			state.mu.Lock()
			delete(state.dashboardRTCChannels, dataChannel)
			state.mu.Unlock()

			atomic.AddInt64(&activeDashboardRTC, -1)
		})
	})

	err = peerConnection.SetRemoteDescription(offer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	err = peerConnection.SetLocalDescription(answer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	<-gatherComplete

	localDescription := peerConnection.LocalDescription()
	answerBytes, err := json.Marshal(localDescription)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, WebRTCSignal{
		SDP: base64.StdEncoding.EncodeToString(answerBytes),
	})
}

func visualizationAckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	var ack VisualizationAck
	err := json.NewDecoder(r.Body).Decode(&ack)
	if err != nil {
		http.Error(w, "Invalid visualization ack JSON", http.StatusBadRequest)
		return
	}

	networkIngestion := ack.T1ServerReceivedMs - ack.T0ClientGeneratedMs
	serverProcessing := ack.T2AvailableForDashboardMs - ack.T1ServerReceivedMs
	dashboardDelivery := ack.T3BrowserReceivedMs - ack.T2AvailableForDashboardMs
	domUpdate := ack.T4DomUpdatedMs - ack.T3BrowserReceivedMs
	paintDelay := ack.T5PaintCompletedMs - ack.T4DomUpdatedMs
	endToEnd := ack.T5PaintCompletedMs - ack.T0ClientGeneratedMs

	visualizationMu.Lock()
	defer visualizationMu.Unlock()

	file, err := os.OpenFile(visualizationFile, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Println("Could not open visualization file:", err)
		http.Error(w, "Could not open visualization file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	row := []string{
		ack.Technology,
		ack.DataChannelMode,
		ack.DashboardMode,
		strconv.Itoa(ack.ScenarioClients),
		strconv.Itoa(ack.Hz),
		ack.RunID,
		strconv.FormatInt(ack.GlobalSequence, 10),
		ack.MessageID,
		strconv.Itoa(ack.ClientID),
		strconv.FormatInt(ack.Sequence, 10),
		strconv.FormatInt(ack.T0ClientGeneratedMs, 10),
		strconv.FormatInt(ack.T1ServerReceivedMs, 10),
		strconv.FormatInt(ack.T2AvailableForDashboardMs, 10),
		strconv.FormatInt(ack.T3BrowserReceivedMs, 10),
		strconv.FormatInt(ack.T4DomUpdatedMs, 10),
		strconv.FormatInt(ack.T5PaintCompletedMs, 10),
		strconv.FormatInt(networkIngestion, 10),
		strconv.FormatInt(serverProcessing, 10),
		strconv.FormatInt(dashboardDelivery, 10),
		strconv.FormatInt(domUpdate, 10),
		strconv.FormatInt(paintDelay, 10),
		strconv.FormatInt(endToEnd, 10),
	}

	writer.Write(row)

	writeJSON(w, map[string]string{
		"status": "ok",
	})
}

func resourceLogger() {
	var previousCPUSeconds float64
	var previousTime = time.Now()

	for {
		time.Sleep(1 * time.Second)

		var usage syscall.Rusage
		err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
		if err != nil {
			continue
		}

		userCPU := float64(usage.Utime.Sec) + float64(usage.Utime.Usec)/1_000_000
		systemCPU := float64(usage.Stime.Sec) + float64(usage.Stime.Usec)/1_000_000
		currentCPUSeconds := userCPU + systemCPU

		currentTime := time.Now()
		elapsed := currentTime.Sub(previousTime).Seconds()

		cpuPercent := 0.0
		if elapsed > 0 {
			cpuPercent = ((currentCPUSeconds - previousCPUSeconds) / elapsed) * 100
		}

		previousCPUSeconds = currentCPUSeconds
		previousTime = currentTime

		ramMB := float64(usage.Maxrss) / 1024.0

		activeIngest := atomic.LoadInt64(&activeIngestWS) + atomic.LoadInt64(&activeIngestRTC)
		activeDashboard := atomic.LoadInt64(&activeDashboardWS) + atomic.LoadInt64(&activeDashboardRTC)

		resourcesMu.Lock()

		file, err := os.OpenFile(resourcesFile, os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			writer := csv.NewWriter(file)

			row := []string{
				strconv.FormatInt(nowMs(), 10),
				technology,
				dataChannelMode,
				dashboardMode,
				strconv.Itoa(scenarioClients),
				strconv.Itoa(hz),
				runID,
				fmt.Sprintf("%.2f", cpuPercent),
				fmt.Sprintf("%.2f", ramMB),
				strconv.Itoa(runtime.NumGoroutine()),
				strconv.FormatInt(activeIngest, 10),
				strconv.FormatInt(activeDashboard, 10),
				strconv.FormatInt(atomic.LoadInt64(&receivedMessagesTotal), 10),
			}

			writer.Write(row)
			writer.Flush()
			file.Close()
		}

		resourcesMu.Unlock()
	}
}

func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = dashboardMode
	}

	dcMode := r.URL.Query().Get("dcMode")
	if dcMode == "" {
		dcMode = dataChannelMode
	}

	html := `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>BA2 Live Tracking Dashboard</title>
  <style>
    body {
      font-family: Arial, sans-serif;
      margin: 24px;
      background: #f6f7f9;
      color: #222;
    }
    .card {
      background: white;
      border-radius: 8px;
      padding: 16px;
      margin-bottom: 16px;
      box-shadow: 0 1px 4px rgba(0,0,0,0.12);
    }
    table {
      border-collapse: collapse;
      width: 100%;
      background: white;
    }
    th, td {
      padding: 8px;
      border-bottom: 1px solid #ddd;
      font-size: 14px;
    }
    th {
      text-align: left;
      background: #eeeeee;
    }
    .ok { color: green; font-weight: bold; }
    .warn { color: orange; font-weight: bold; }
  </style>
</head>
<body>
  <h1>BA2 Live Tracking Dashboard</h1>

  <div class="card">
    <p><strong>Technology:</strong> <span id="technology">-</span></p>
    <p><strong>DataChannel Mode:</strong> <span id="dataChannelMode">-</span></p>
    <p><strong>Dashboard Mode:</strong> <span id="dashboardMode">-</span></p>
    <p><strong>Scenario Clients:</strong> <span id="scenarioClients">-</span></p>
    <p><strong>Hz:</strong> <span id="hz">-</span></p>
    <p><strong>Run ID:</strong> <span id="runId">-</span></p>
    <p><strong>Status:</strong> <span id="connectionStatus" class="warn">connecting</span></p>
    <p><strong>Rendered Updates:</strong> <span id="renderedUpdates">0</span></p>
    <p><strong>Last Global Sequence:</strong> <span id="lastGlobalSequence">0</span></p>
    <p><strong>Clock Offset Browser → Server:</strong> <span id="clockOffset">-</span> ms</p>
  </div>

  <div class="card">
    <h2>Latest Positions</h2>
    <table>
      <thead>
        <tr>
          <th>Global Seq</th>
          <th>Message ID</th>
          <th>Client ID</th>
          <th>Seq</th>
          <th>Latitude</th>
          <th>Longitude</th>
          <th>t1 - t0</th>
        </tr>
      </thead>
      <tbody id="positions"></tbody>
    </table>
  </div>

<script>
const configuredTechnology = "{{TECHNOLOGY}}";
const configuredDataChannelMode = "{{DATA_CHANNEL_MODE}}";
const configuredDashboardMode = "{{DASHBOARD_MODE}}";
const configuredScenarioClients = Number("{{SCENARIO_CLIENTS}}");
const configuredHz = Number("{{HZ}}");
const configuredRunId = "{{RUN_ID}}";
const configuredModeFromUrl = "{{MODE_FROM_URL}}";
const configuredDcModeFromUrl = "{{DC_MODE_FROM_URL}}";

let serverOffsetMs = 0;
let renderedUpdates = 0;
let lastSeenGlobalSequence = 0;
let latestByClient = {};

document.getElementById('technology').innerText = configuredTechnology;
document.getElementById('dataChannelMode').innerText = configuredDataChannelMode || '-';
document.getElementById('dashboardMode').innerText = configuredModeFromUrl;
document.getElementById('scenarioClients').innerText = configuredScenarioClients;
document.getElementById('hz').innerText = configuredHz;
document.getElementById('runId').innerText = configuredRunId;

function nowServerEstimated() {
  return Date.now() + serverOffsetMs;
}

async function calibrateClock() {
  try {
    const tStart = Date.now();
    const response = await fetch('/api/clock');
    const data = await response.json();
    const tEnd = Date.now();

    const midpoint = (tStart + tEnd) / 2;
    serverOffsetMs = data.serverTimeMs - midpoint;

    document.getElementById('clockOffset').innerText = serverOffsetMs.toFixed(2);
  } catch (e) {
    document.getElementById('clockOffset').innerText = 'error';
  }
}

function setStatus(text, ok) {
  const el = document.getElementById('connectionStatus');
  el.innerText = text;
  el.className = ok ? 'ok' : 'warn';
}

let lastTableRenderMs = 0;

function renderPosition(update) {
  latestByClient[update.clientId] = update;

  const now = Date.now();

  // Die Tabelle wird maximal 2-mal pro Sekunde aktualisiert.
  // Die Messung und der Visualization-Ack laufen trotzdem für jedes Update.
  if (now - lastTableRenderMs < 500) {
    return;
  }

  lastTableRenderMs = now;

  const tbody = document.getElementById('positions');
  tbody.innerHTML = '';

  const positions = Object.values(latestByClient)
    .sort((a, b) => a.clientId - b.clientId)
    .slice(0, 50);

  for (const pos of positions) {
    const row = document.createElement('tr');

    const latency = pos.t1ServerReceivedMs - pos.t0ClientGeneratedMs;

    row.innerHTML =
      '<td>' + pos.globalSequence + '</td>' +
      '<td>' + pos.messageId + '</td>' +
      '<td>' + pos.clientId + '</td>' +
      '<td>' + pos.sequence + '</td>' +
      '<td>' + pos.latitude.toFixed(6) + '</td>' +
      '<td>' + pos.longitude.toFixed(6) + '</td>' +
      '<td>' + latency + ' ms</td>';

    tbody.appendChild(row);
  }
}

function handleUpdate(update) {
  const t3 = nowServerEstimated();

  renderPosition(update);

  const t4 = nowServerEstimated();

  requestAnimationFrame(function() {
    requestAnimationFrame(function() {
      const t5 = nowServerEstimated();

      renderedUpdates += 1;
      lastSeenGlobalSequence = Math.max(lastSeenGlobalSequence, update.globalSequence);

      document.getElementById('renderedUpdates').innerText = renderedUpdates;
      document.getElementById('lastGlobalSequence').innerText = lastSeenGlobalSequence;

      sendVisualizationAck(update, t3, t4, t5);
    });
  });
}

function sendVisualizationAck(update, t3, t4, t5) {
  fetch('/api/visualization-ack', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({
      technology: update.technology,
      dataChannelMode: update.dataChannelMode,
      dashboardMode: update.dashboardMode,
      scenarioClients: configuredScenarioClients,
      hz: configuredHz,
      runId: configuredRunId,

      globalSequence: update.globalSequence,
      messageId: update.messageId,
      clientId: update.clientId,
      sequence: update.sequence,

      t0ClientGeneratedMs: update.t0ClientGeneratedMs,
      t1ServerReceivedMs: update.t1ServerReceivedMs,
      t2AvailableForDashboardMs: update.t2AvailableForDashboardMs,
      t3BrowserReceivedMs: Math.round(t3),
      t4DomUpdatedMs: Math.round(t4),
      t5PaintCompletedMs: Math.round(t5)
    })
  }).catch(function() {});
}

async function startLongPolling() {
  setStatus('long polling connecting', false);

  while (true) {
    try {
      const response = await fetch('/dashboard/long-poll?lastSeenGlobalSequence=' + lastSeenGlobalSequence);
      const data = await response.json();

      setStatus('long polling connected', true);

      if (data.updates) {
        for (const update of data.updates) {
          handleUpdate(update);
        }
      }
    } catch (e) {
      setStatus('long polling error, retrying', false);
      await new Promise(function(resolve) {
        setTimeout(resolve, 1000);
      });
    }
  }
}

function startWebSocketDashboard() {
  const protocol = window.location.protocol === 'https:' ? 'wss://' : 'ws://';
  const ws = new WebSocket(protocol + window.location.host + '/dashboard/ws');

  ws.onopen = function() {
    setStatus('websocket connected', true);
  };

  ws.onerror = function() {
    setStatus('websocket error', false);
  };

  ws.onclose = function() {
    setStatus('websocket closed', false);
  };

  ws.onmessage = function(event) {
    const update = JSON.parse(event.data);
    handleUpdate(update);
  };
}

async function waitForIceGatheringComplete(pc) {
  if (pc.iceGatheringState === 'complete') {
    return;
  }

  await new Promise(function(resolve) {
    function checkState() {
      if (pc.iceGatheringState === 'complete') {
        pc.removeEventListener('icegatheringstatechange', checkState);
        resolve();
      }
    }
    pc.addEventListener('icegatheringstatechange', checkState);
  });
}

async function startWebRTCDashboard() {
  setStatus('webrtc connecting', false);

  const pc = new RTCPeerConnection();

  let options = {};
  if (configuredDcModeFromUrl === 'unreliable-unordered') {
    options = {
      ordered: false,
      maxRetransmits: 0
    };
  }

  const dc = pc.createDataChannel('dashboard', options);

  dc.onopen = function() {
    setStatus('webrtc datachannel connected', true);
  };

  dc.onclose = function() {
    setStatus('webrtc datachannel closed', false);
  };

  dc.onerror = function() {
    setStatus('webrtc datachannel error', false);
  };

  dc.onmessage = function(event) {
    const update = JSON.parse(event.data);
    handleUpdate(update);
  };

  const offer = await pc.createOffer();
  await pc.setLocalDescription(offer);
  await waitForIceGatheringComplete(pc);

  const encodedOffer = btoa(JSON.stringify(pc.localDescription));

  const response = await fetch('/dashboard/webrtc/offer', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({
      sdp: encodedOffer
    })
  });

  const answerSignal = await response.json();
  const answer = JSON.parse(atob(answerSignal.sdp));

  await pc.setRemoteDescription(answer);
}

async function startDashboard() {
  await calibrateClock();

  if (configuredModeFromUrl === 'long-polling') {
    startLongPolling();
  } else if (configuredModeFromUrl === 'websocket') {
    startWebSocketDashboard();
  } else if (configuredModeFromUrl === 'webrtc') {
    startWebRTCDashboard();
  } else {
    setStatus('unknown dashboard mode', false);
  }
}

startDashboard();
</script>
</body>
</html>`

	replacements := map[string]string{
		"{{TECHNOLOGY}}":        technology,
		"{{DATA_CHANNEL_MODE}}": dataChannelMode,
		"{{DASHBOARD_MODE}}":    dashboardMode,
		"{{SCENARIO_CLIENTS}}":  strconv.Itoa(scenarioClients),
		"{{HZ}}":                strconv.Itoa(hz),
		"{{RUN_ID}}":            runID,
		"{{MODE_FROM_URL}}":     mode,
		"{{DC_MODE_FROM_URL}}":  dcMode,
	}

	for key, value := range replacements {
		html = strings.ReplaceAll(html, key, value)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

func writeJSON(w http.ResponseWriter, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}
