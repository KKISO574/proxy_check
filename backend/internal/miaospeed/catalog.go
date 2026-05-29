package miaospeed

const (
	MatrixSpeedAverage    = "SPEED_AVERAGE"
	MatrixSpeedMax        = "SPEED_MAX"
	MatrixSpeedPerSecond  = "SPEED_PER_SECOND"
	MatrixUSpeedAverage   = "USPEED_AVERAGE"
	MatrixUSpeedMax       = "USPEED_MAX"
	MatrixUSpeedPerSecond = "USPEED_PER_SECOND"
	MatrixHTTPPing        = "TEST_PING_CONN"
	MatrixRTTPing         = "TEST_PING_RTT"
	MatrixMaxRTTPing      = "TEST_PING_MAX_RTT"
	MatrixTotalHTTPPing   = "TEST_PING_TOTAL_CONN"
	MatrixTotalRTTPing    = "TEST_PING_TOTAL_RTT"
	MatrixSDRTT           = "TEST_PING_SD_RTT"
	MatrixSDHTTP          = "TEST_PING_SD_CONN"
	MatrixHTTPCode        = "TEST_HTTP_CODE"
	MatrixPacketLoss      = "TEST_PING_PACKET_LOSS"
	MatrixHijack          = "TEST_HIJACK_DETECTION"
	MatrixUDPType         = "UDP_TYPE"
	MatrixInboundGeoIP    = "GEOIP_INBOUND"
	MatrixOutboundGeoIP   = "GEOIP_OUTBOUND"
	MatrixScriptTest      = "TEST_SCRIPT"
)

type ServiceTestDefinition struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	ScriptID string `json:"script_id"`
	Group    string `json:"group"`
}

func DefaultServiceCatalog() []ServiceTestDefinition {
	return []ServiceTestDefinition{
		{Key: "netflix", Label: "Netflix", ScriptID: "netflix_unlock", Group: "streaming"},
		{Key: "disney", Label: "Disney+", ScriptID: "disney_unlock", Group: "streaming"},
		{Key: "youtube", Label: "YouTube", ScriptID: "youtube_unlock", Group: "streaming"},
		{Key: "tiktok", Label: "TikTok", ScriptID: "tiktok_unlock", Group: "streaming"},
		{Key: "openai", Label: "OpenAI", ScriptID: "openai_unlock", Group: "ai"},
		{Key: "google", Label: "Google", ScriptID: "google_unlock", Group: "service"},
		{Key: "github", Label: "GitHub", ScriptID: "github_unlock", Group: "service"},
		{Key: "telegram", Label: "Telegram", ScriptID: "telegram_unlock", Group: "service"},
		{Key: "spotify", Label: "Spotify", ScriptID: "spotify_unlock", Group: "streaming"},
		{Key: "steam", Label: "Steam", ScriptID: "steam_unlock", Group: "gaming"},
		{Key: "bilibili", Label: "Bilibili", ScriptID: "bilibili_unlock", Group: "streaming"},
		{Key: "abema", Label: "Abema", ScriptID: "abema_unlock", Group: "streaming"},
		{Key: "dazn", Label: "DAZN", ScriptID: "dazn_unlock", Group: "streaming"},
		{Key: "hulu", Label: "Hulu", ScriptID: "hulu_unlock", Group: "streaming"},
		{Key: "prime_video", Label: "Prime Video", ScriptID: "prime_video_unlock", Group: "streaming"},
		{Key: "hbo_max", Label: "HBO Max", ScriptID: "hbo_max_unlock", Group: "streaming"},
		{Key: "bahamut", Label: "Bahamut", ScriptID: "bahamut_unlock", Group: "streaming"},
		{Key: "bbc_iplayer", Label: "BBC iPlayer", ScriptID: "bbc_iplayer_unlock", Group: "streaming"},
		{Key: "claude", Label: "Claude", ScriptID: "claude_unlock", Group: "ai"},
		{Key: "gemini", Label: "Gemini", ScriptID: "gemini_unlock", Group: "ai"},
	}
}
