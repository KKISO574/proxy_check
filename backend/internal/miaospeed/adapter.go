package miaospeed

import (
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type Matrix struct {
	Type   string
	Params string
}

func (m Matrix) ToWire() map[string]string {
	return map[string]string{"Type": m.Type, "Params": m.Params}
}

type Node struct {
	Name    string
	Payload string
}

func (n Node) ToWire() map[string]string {
	return map[string]string{"Name": n.Name, "Payload": n.Payload}
}

type RequestConfig struct {
	ApiVersion        int
	DownloadURL       string
	DownloadDuration  int
	DownloadThreading int
	UploadURL         string
	UploadDuration    int
	UploadThreading   int
	PingAverageOver   int
	PingAddress       string
	TaskRetry         int
	DNSServers        []string
	TaskTimeout       int
	Scripts           []Script
}

type Script struct {
	ID            string
	Type          string
	Content       string
	TimeoutMillis uint64
}

func (s Script) ToWire() map[string]any {
	return map[string]any{
		"ID":            s.ID,
		"Type":          s.Type,
		"Content":       s.Content,
		"TimeoutMillis": s.TimeoutMillis,
	}
}

func (c RequestConfig) ToWire() map[string]any {
	config := map[string]any{}
	config["ApiVersion"] = c.apiVersion()
	putString(config, "DownloadURL", c.DownloadURL)
	putInt(config, "DownloadDuration", c.DownloadDuration)
	putInt(config, "DownloadThreading", c.DownloadThreading)
	putString(config, "UploadURL", c.UploadURL)
	putInt(config, "UploadDuration", c.UploadDuration)
	putInt(config, "UploadThreading", c.UploadThreading)
	putInt(config, "PingAverageOver", c.PingAverageOver)
	putString(config, "PingAddress", c.PingAddress)
	putInt(config, "TaskRetry", c.TaskRetry)
	if len(c.DNSServers) > 0 {
		config["DNSServers"] = c.DNSServers
	}
	putInt(config, "TaskTimeout", c.TaskTimeout)
	if len(c.Scripts) > 0 {
		scripts := make([]map[string]any, 0, len(c.Scripts))
		for _, script := range c.Scripts {
			scripts = append(scripts, script.ToWire())
		}
		config["Scripts"] = scripts
	}
	return config
}

func (c RequestConfig) apiVersion() int {
	if c.ApiVersion > 0 {
		return c.ApiVersion
	}
	return 3
}

type Request struct {
	TaskID         string
	Invoker        string
	Vendor         string
	Nodes          []Node
	Matrices       []Matrix
	Config         RequestConfig
	RandomSequence string
	Challenge      string
	Slave          string
	SlaveName      string
	Version        string
	FilterText     string
}

func BuildRequest(request Request) map[string]any {
	version := request.Version
	if version == "" {
		version = request.Invoker
	}
	nodes := make([]map[string]string, 0, len(request.Nodes))
	for _, node := range request.Nodes {
		nodes = append(nodes, node.ToWire())
	}
	matrices := make([]map[string]string, 0, len(request.Matrices))
	for _, matrix := range request.Matrices {
		matrices = append(matrices, matrix.ToWire())
	}
	return map[string]any{
		"Basics": map[string]any{
			"ID":        request.TaskID,
			"Slave":     request.Slave,
			"SlaveName": request.SlaveName,
			"Invoker":   request.Invoker,
			"Version":   version,
		},
		"Options": map[string]any{
			"Filter":   request.FilterText,
			"Matrices": matrices,
		},
		"Configs":        request.Config.ToWire(),
		"Vendor":         request.Vendor,
		"Nodes":          nodes,
		"RandomSequence": request.RandomSequence,
		"Challenge":      request.Challenge,
	}
}

type MatrixResult struct {
	Type       string
	Payload    any
	RawPayload any
}

type NodeResult struct {
	Name             string
	ProxyInfo        map[string]any
	InvokeDurationMS int
	Matrices         map[string]MatrixResult
}

func (n NodeResult) AverageSpeedMbps() *float64 {
	if value := matrixMbpsValue(n.Matrices[MatrixSpeedAverage], "Value"); value != nil {
		return value
	}
	return matrixMbpsValue(n.Matrices[MatrixSpeedPerSecond], "Average")
}

func (n NodeResult) MaxSpeedMbps() *float64 {
	if value := matrixMbpsValue(n.Matrices[MatrixSpeedMax], "Value"); value != nil {
		return value
	}
	return matrixMbpsValue(n.Matrices[MatrixSpeedPerSecond], "Max")
}

func (n NodeResult) AverageUploadMbps() *float64 {
	if value := matrixMbpsValue(n.Matrices[MatrixUSpeedAverage], "Value"); value != nil {
		return value
	}
	return matrixMbpsValue(n.Matrices[MatrixUSpeedPerSecond], "Average")
}

func (n NodeResult) MaxUploadMbps() *float64 {
	if value := matrixMbpsValue(n.Matrices[MatrixUSpeedMax], "Value"); value != nil {
		return value
	}
	return matrixMbpsValue(n.Matrices[MatrixUSpeedPerSecond], "Max")
}

func (n NodeResult) MatrixNumber(matrixName string, key string) *float64 {
	return matrixValue(n.Matrices[matrixName], key)
}

func (n NodeResult) MatrixString(matrixName string, keys ...string) string {
	matrix := n.Matrices[matrixName]
	switch payload := matrix.Payload.(type) {
	case string:
		return payload
	case map[string]any:
		for _, key := range keys {
			if value, ok := payload[key]; ok && value != nil {
				return fmt.Sprint(value)
			}
		}
	}
	return ""
}

type Frame struct {
	ID              string
	Version         string
	IsFinal         bool
	Nodes           []NodeResult
	ProgressIndex   *int
	ProgressQueuing *int
}

type FrameHandler func(Frame)

type WebSocketClient struct {
	URL         string
	TimeoutMS   int
	Dialer      *websocket.Dialer
	Token       string
	BuildTokens []string
}

type ClientOption func(*WebSocketClient)

func WithToken(token string) ClientOption {
	return func(client *WebSocketClient) {
		client.Token = token
	}
}

func WithBuildTokens(tokens []string) ClientOption {
	return func(client *WebSocketClient) {
		client.BuildTokens = append([]string{}, tokens...)
	}
}

func NewWebSocketClient(wsURL string, timeoutMS int, options ...ClientOption) *WebSocketClient {
	client := &WebSocketClient{URL: wsURL, TimeoutMS: timeoutMS}
	for _, option := range options {
		option(client)
	}
	return client
}

func (c *WebSocketClient) Run(ctx context.Context, request map[string]any, onFrame FrameHandler) (Frame, error) {
	if c.URL == "" {
		return Frame{}, errors.New("miaospeed websocket URL is not configured")
	}
	timeout := time.Duration(c.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dialer := c.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	conn, _, err := dialer.DialContext(ctx, c.URL, nil)
	if err != nil {
		return Frame{}, err
	}
	defer conn.Close()

	wireRequest := cloneWireMap(request)
	if c.Token != "" {
		challenge, err := SignWireRequest(c.Token, c.BuildTokens, wireRequest)
		if err != nil {
			return Frame{}, err
		}
		wireRequest["Challenge"] = challenge
	}
	if err := conn.WriteJSON(wireRequest); err != nil {
		return Frame{}, err
	}
	for {
		var raw map[string]any
		if err := conn.ReadJSON(&raw); err != nil {
			return Frame{}, err
		}
		frame, err := NormalizeFrame(raw)
		if err != nil {
			return Frame{}, enrichRunError(err)
		}
		if onFrame != nil {
			onFrame(frame)
		}
		if frame.IsFinal {
			return frame, nil
		}
	}
}

func enrichRunError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	if strings.Contains(message, "cannot verify the request") {
		return fmt.Errorf("%s; check MIAOSPEED_TOKEN and set MIAOSPEED_BUILD_TOKENS to the build token segments used by this MiaoSpeed binary", message)
	}
	return err
}

type wireMatrix struct {
	Type   string
	Params string
}

type wireOptions struct {
	Filter   string
	Matrices []wireMatrix
}

type wireBasics struct {
	ID        string
	Slave     string
	SlaveName string
	Invoker   string
	Version   string
}

type wireConfig struct {
	ApiVersion        int
	STUNURL           string
	DownloadURL       string
	DownloadDuration  int
	DownloadThreading int
	UploadURL         string
	UploadDuration    int
	UploadThreading   int
	PingAverageOver   int
	PingAddress       string
	TaskRetry         int
	DNSServers        []string
	TaskTimeout       int
	Scripts           []Script
}

type wireNode struct {
	Name    string
	Payload string
}

type wireRequest struct {
	Basics         wireBasics
	Options        wireOptions
	Configs        wireConfig
	Vendor         string
	Nodes          []wireNode
	RandomSequence string
	Challenge      string
}

func SignRequest(token string, buildTokens []string, request Request) (string, error) {
	wire := request.ToWire()
	return signWireRequest(token, buildTokens, wire)
}

func SignWireRequest(token string, buildTokens []string, request map[string]any) (string, error) {
	wire, err := wireRequestFromMap(request)
	if err != nil {
		return "", err
	}
	return signWireRequest(token, buildTokens, wire)
}

func (r Request) ToWire() wireRequest {
	version := r.Version
	if version == "" {
		version = r.Invoker
	}
	matrices := make([]wireMatrix, 0, len(r.Matrices))
	for _, matrix := range r.Matrices {
		matrices = append(matrices, wireMatrix{Type: matrix.Type, Params: matrix.Params})
	}
	nodes := make([]wireNode, 0, len(r.Nodes))
	for _, node := range r.Nodes {
		nodes = append(nodes, wireNode{Name: node.Name, Payload: node.Payload})
	}
	return wireRequest{
		Basics: wireBasics{
			ID:        r.TaskID,
			Slave:     r.Slave,
			SlaveName: r.SlaveName,
			Invoker:   r.Invoker,
			Version:   version,
		},
		Options: wireOptions{
			Filter:   r.FilterText,
			Matrices: matrices,
		},
		Configs: wireConfig{
			ApiVersion:        r.Config.apiVersion(),
			DownloadURL:       r.Config.DownloadURL,
			DownloadDuration:  r.Config.DownloadDuration,
			DownloadThreading: r.Config.DownloadThreading,
			UploadURL:         r.Config.UploadURL,
			UploadDuration:    r.Config.UploadDuration,
			UploadThreading:   r.Config.UploadThreading,
			PingAverageOver:   r.Config.PingAverageOver,
			PingAddress:       r.Config.PingAddress,
			TaskRetry:         r.Config.TaskRetry,
			DNSServers:        r.Config.DNSServers,
			TaskTimeout:       r.Config.TaskTimeout,
			Scripts:           append([]Script{}, r.Config.Scripts...),
		},
		Vendor:         r.Vendor,
		Nodes:          nodes,
		RandomSequence: r.RandomSequence,
		Challenge:      r.Challenge,
	}
}

func signWireRequest(token string, buildTokens []string, request wireRequest) (string, error) {
	request.Challenge = ""
	encoded, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	if len(buildTokens) == 0 {
		buildTokens = []string{""}
	}
	hasher := sha512.New()
	hasher.Write(encoded)
	for _, segment := range append([]string{token}, buildTokens...) {
		if segment == "" {
			segment = "SOME_TOKEN"
		}
		hasher.Write(hasher.Sum([]byte(segment)))
	}
	return base64.URLEncoding.EncodeToString(hasher.Sum(nil)), nil
}

func wireRequestFromMap(request map[string]any) (wireRequest, error) {
	wire := wireRequest{
		Basics:  wireBasics{},
		Options: wireOptions{Matrices: []wireMatrix{}},
		Configs: wireConfig{},
		Nodes:   []wireNode{},
	}
	if basics, ok := request["Basics"].(map[string]any); ok {
		wire.Basics = wireBasics{
			ID:        stringValue(basics["ID"]),
			Slave:     stringValue(basics["Slave"]),
			SlaveName: stringValue(basics["SlaveName"]),
			Invoker:   stringValue(basics["Invoker"]),
			Version:   stringValue(basics["Version"]),
		}
	}
	if options, ok := request["Options"].(map[string]any); ok {
		wire.Options.Filter = stringValue(options["Filter"])
		wire.Options.Matrices = matrixSlice(options["Matrices"])
	}
	if configs, ok := request["Configs"].(map[string]any); ok {
		wire.Configs = wireConfig{
			ApiVersion:        intValue(configs["ApiVersion"]),
			STUNURL:           stringValue(configs["STUNURL"]),
			DownloadURL:       stringValue(configs["DownloadURL"]),
			DownloadDuration:  intValue(configs["DownloadDuration"]),
			DownloadThreading: intValue(configs["DownloadThreading"]),
			UploadURL:         stringValue(configs["UploadURL"]),
			UploadDuration:    intValue(configs["UploadDuration"]),
			UploadThreading:   intValue(configs["UploadThreading"]),
			PingAverageOver:   intValue(configs["PingAverageOver"]),
			PingAddress:       stringValue(configs["PingAddress"]),
			TaskRetry:         intValue(configs["TaskRetry"]),
			DNSServers:        stringSlice(configs["DNSServers"]),
			TaskTimeout:       intValue(configs["TaskTimeout"]),
			Scripts:           scriptSlice(configs["Scripts"]),
		}
	}
	wire.Vendor = stringValue(request["Vendor"])
	wire.Nodes = nodeSlice(request["Nodes"])
	wire.RandomSequence = stringValue(request["RandomSequence"])
	wire.Challenge = stringValue(request["Challenge"])
	return wire, nil
}

func cloneWireMap(request map[string]any) map[string]any {
	clone := make(map[string]any, len(request))
	for key, value := range request {
		clone[key] = value
	}
	return clone
}

func (c *WebSocketClient) Ping(ctx context.Context) error {
	if c.URL == "" {
		return errors.New("miaospeed websocket URL is not configured")
	}
	timeout := time.Duration(c.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dialer := c.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	conn, _, err := dialer.DialContext(ctx, c.URL, nil)
	if err != nil {
		return err
	}
	return conn.Close()
}

func NormalizeFrame(raw map[string]any) (Frame, error) {
	if value, ok := raw["Error"]; ok && fmt.Sprint(value) != "" {
		return Frame{}, errors.New(fmt.Sprint(value))
	}
	frame := Frame{
		ID:      stringValue(raw["ID"]),
		Version: stringValue(raw["MiaoSpeedVersion"]),
		Nodes:   []NodeResult{},
	}
	if progress, ok := raw["Progress"].(map[string]any); ok {
		frame.IsFinal = false
		frame.ProgressIndex = optionalInt(progress["Index"])
		frame.ProgressQueuing = optionalInt(progress["Queuing"])
		if record, ok := progress["Record"].(map[string]any); ok {
			frame.Nodes = append(frame.Nodes, normalizeNodeRecord(record))
		}
		return frame, nil
	}
	if result, ok := raw["Result"].(map[string]any); ok {
		frame.IsFinal = true
		if records, ok := result["Results"].([]any); ok {
			for _, item := range records {
				record, ok := item.(map[string]any)
				if !ok {
					continue
				}
				frame.Nodes = append(frame.Nodes, normalizeNodeRecord(record))
			}
		}
	}
	return frame, nil
}

func normalizeNodeRecord(record map[string]any) NodeResult {
	proxyInfo, _ := record["ProxyInfo"].(map[string]any)
	if proxyInfo == nil {
		proxyInfo = map[string]any{}
	}
	matrices := map[string]MatrixResult{}
	if rawMatrices, ok := record["Matrices"].([]any); ok {
		for _, item := range rawMatrices {
			matrix, ok := item.(map[string]any)
			if !ok {
				continue
			}
			matrixType := stringValue(matrix["Type"])
			if matrixType == "" {
				continue
			}
			rawPayload := matrix["Payload"]
			payload := parsePayload(rawPayload)
			matrixKey := matrixType
			if matrixType == "TEST_SCRIPT" {
				if payloadMap, ok := payload.(map[string]any); ok {
					if scriptKey := stringValue(payloadMap["Key"]); scriptKey != "" {
						matrixKey = matrixType + ":" + scriptKey
					}
				}
			}
			matrices[matrixKey] = MatrixResult{
				Type:       matrixType,
				Payload:    payload,
				RawPayload: rawPayload,
			}
		}
	}
	name := stringValue(proxyInfo["Name"])
	if name == "" {
		name = stringValue(record["Name"])
	}
	return NodeResult{
		Name:             name,
		ProxyInfo:        proxyInfo,
		InvokeDurationMS: intValue(record["InvokeDuration"]),
		Matrices:         matrices,
	}
}

func parsePayload(payload any) any {
	text, ok := payload.(string)
	if !ok {
		return payload
	}
	var decoded any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return payload
	}
	return decoded
}

func matrixValue(matrix MatrixResult, key string) *float64 {
	switch payload := matrix.Payload.(type) {
	case float64:
		return &payload
	case int:
		value := float64(payload)
		return &value
	case map[string]any:
		if keyValue, ok := payload[key]; ok {
			return floatPointer(keyValue)
		}
	}
	return nil
}

func matrixMbpsValue(matrix MatrixResult, key string) *float64 {
	value := matrixValue(matrix, key)
	if value == nil {
		return nil
	}
	converted := *value * 8 / 1_000_000
	return &converted
}

func floatPointer(value any) *float64 {
	switch item := value.(type) {
	case float64:
		return &item
	case int:
		converted := float64(item)
		return &converted
	case json.Number:
		converted, err := item.Float64()
		if err != nil {
			return nil
		}
		return &converted
	default:
		return nil
	}
}

func matrixSlice(value any) []wireMatrix {
	switch items := value.(type) {
	case []map[string]string:
		matrices := make([]wireMatrix, 0, len(items))
		for _, item := range items {
			matrices = append(matrices, wireMatrix{Type: item["Type"], Params: item["Params"]})
		}
		return matrices
	case []any:
		matrices := make([]wireMatrix, 0, len(items))
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			matrices = append(matrices, wireMatrix{Type: stringValue(item["Type"]), Params: stringValue(item["Params"])})
		}
		return matrices
	default:
		return []wireMatrix{}
	}
}

func nodeSlice(value any) []wireNode {
	switch items := value.(type) {
	case []map[string]string:
		nodes := make([]wireNode, 0, len(items))
		for _, item := range items {
			nodes = append(nodes, wireNode{Name: item["Name"], Payload: item["Payload"]})
		}
		return nodes
	case []any:
		nodes := make([]wireNode, 0, len(items))
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			nodes = append(nodes, wireNode{Name: stringValue(item["Name"]), Payload: stringValue(item["Payload"])})
		}
		return nodes
	default:
		return []wireNode{}
	}
}

func scriptSlice(value any) []Script {
	switch items := value.(type) {
	case []map[string]any:
		scripts := make([]Script, 0, len(items))
		for _, item := range items {
			scripts = append(scripts, Script{
				ID:            stringValue(item["ID"]),
				Type:          stringValue(item["Type"]),
				Content:       stringValue(item["Content"]),
				TimeoutMillis: uint64(intValue(item["TimeoutMillis"])),
			})
		}
		return scripts
	case []any:
		scripts := make([]Script, 0, len(items))
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			scripts = append(scripts, Script{
				ID:            stringValue(item["ID"]),
				Type:          stringValue(item["Type"]),
				Content:       stringValue(item["Content"]),
				TimeoutMillis: uint64(intValue(item["TimeoutMillis"])),
			})
		}
		return scripts
	default:
		return nil
	}
}

func stringSlice(value any) []string {
	switch items := value.(type) {
	case []string:
		return append([]string{}, items...)
	case []any:
		values := make([]string, 0, len(items))
		for _, item := range items {
			values = append(values, stringValue(item))
		}
		return values
	default:
		return nil
	}
}

func optionalInt(value any) *int {
	converted := intValue(value)
	return &converted
}

func intValue(value any) int {
	switch item := value.(type) {
	case int:
		return item
	case int64:
		return int(item)
	case uint:
		return int(item)
	case uint64:
		return int(item)
	case float64:
		return int(item)
	case json.Number:
		converted, _ := item.Int64()
		return int(converted)
	default:
		return 0
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func putString(values map[string]any, key string, value string) {
	if value != "" {
		values[key] = value
	}
}

func putInt(values map[string]any, key string, value int) {
	if value != 0 {
		values[key] = value
	}
}
