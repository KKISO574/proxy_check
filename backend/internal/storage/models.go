package storage

type MetricSummary struct {
	Metric    string   `json:"metric"`
	Target    string   `json:"target"`
	LatencyMS *float64 `json:"latency_ms"`
	Value     *float64 `json:"value"`
	Data      *string  `json:"data"`
	Success   bool     `json:"success"`
	Error     *string  `json:"error"`
	CreatedAt string   `json:"created_at"`
}

type NodeMeta struct {
	ExitIP        *string `json:"exit_ip"`
	ASN           *string `json:"asn"`
	Country       *string `json:"country"`
	Region        *string `json:"region"`
	ISP           *string `json:"isp"`
	NetflixUnlock *string `json:"netflix_unlock"`
	DisneyUnlock  *string `json:"disney_unlock"`
	OpenAIUnlock  *string `json:"openai_unlock"`
	YouTubeUnlock *string `json:"youtube_unlock"`
	DNSLeak       *string `json:"dns_leak"`
}

type Node struct {
	ID            int                       `json:"id"`
	TaskID        *int                      `json:"task_id"`
	Name          string                    `json:"name"`
	Type          *string                   `json:"type"`
	Server        *string                   `json:"server"`
	Port          *int                      `json:"port"`
	RawConfig     *string                   `json:"-"`
	ListenerPort  *int                      `json:"listener_port"`
	Status        string                    `json:"status"`
	Metrics       map[string]MetricSummary  `json:"metrics"`
	Meta          *NodeMeta                 `json:"meta"`
	Score         *float64                  `json:"score"`
	Confidence    float64                   `json:"score_confidence"`
	Breakdown     map[string]ScoreComponent `json:"score_breakdown"`
	LastCheckedAt *string                   `json:"last_checked_at"`
}

type Task struct {
	ID                    int     `json:"id"`
	Name                  string  `json:"name"`
	SourceURL             string  `json:"source_url"`
	ConfigPath            string  `json:"-"`
	Enabled               bool    `json:"enabled"`
	IntervalSeconds       int     `json:"interval_seconds"`
	AdvancedProbesEnabled bool    `json:"advanced_probes_enabled"`
	Status                string  `json:"status"`
	NodeCount             int     `json:"node_count"`
	LastRefreshAt         *string `json:"last_refresh_at"`
	LastRefreshError      *string `json:"last_refresh_error"`
	LastCheckedAt         *string `json:"last_checked_at"`
	NextRunAt             *string `json:"next_run_at"`
}

type TaskPatch struct {
	Name                  *string
	SourceURL             *string
	ConfigPath            *string
	Enabled               *bool
	IntervalSeconds       *int
	AdvancedProbesEnabled *bool
	Status                *string
	LastRefreshAt         *string
	LastRefreshError      *string
	ClearLastRefreshError bool
	LastCheckedAt         *string
	NextRunAt             *string
}

type Stats struct {
	TotalNodes     int      `json:"total_nodes"`
	AvailableNodes int      `json:"available_nodes"`
	DownNodes      int      `json:"down_nodes"`
	UnknownNodes   int      `json:"unknown_nodes"`
	AverageDelayMS *float64 `json:"average_delay_ms"`
}

type RunSummary struct {
	Nodes   int `json:"nodes"`
	Results int `json:"results"`
	Errors  int `json:"errors"`
}

type NodeInput struct {
	Name      string
	Type      *string
	Server    *string
	Port      *int
	RawConfig string
}

type ProbeResultInput struct {
	Metric    string
	Target    string
	LatencyMS *float64
	Value     *float64
	Data      *string
	Success   bool
	Error     *string
}

type ScoreComponent struct {
	Weight       float64  `json:"weight"`
	Score        float64  `json:"score"`
	Contribution float64  `json:"contribution"`
	Value        *float64 `json:"value"`
	Status       string   `json:"status"`
}
