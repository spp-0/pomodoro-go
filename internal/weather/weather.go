package weather

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// httpClient 强制使用 HTTP/1.1，并优先尝试 IPv4。
// 经验：部分国内网络对海外域名走 IPv6 会长时间挂起，而 IPv4 可正常连通；
// HTTP/2 在这些链路上也偶发抖动。实测 IPv4 + HTTP/1.1 最稳。
// 每个请求 5s 客户端超时，避免单请求无限挂起。
var httpClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		TLSNextProto: make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// 优先 IPv4；若失败再回退到默认双栈。
			d := &net.Dialer{Timeout: 5 * time.Second}
			if conn, err := d.DialContext(ctx, "tcp4", addr); err == nil {
				return conn, nil
			}
			return d.DialContext(ctx, network, addr)
		},
	},
}

// getURL 发起 GET 请求，对网络/超时类错误重试一次（open-meteo 在部分网络下偶发抖动）。
// 返回成功（2xx）的 *http.Response，调用方负责关闭 Body。
func getURL(ctx context.Context, urlStr string) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
		if err != nil {
			return nil, err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, urlStr)
		}
		return resp, nil
	}
	return nil, lastErr
}

// Weather 表示某城市当前天气。
type Weather struct {
	City        string
	Temperature float64
	Code        int    // WMO weather code
	Text        string // 中文天气文案
	Emoji       string // 对应 emoji
	IsDay       int
}

// Fetch 通过 Open-Meteo（无需 API key）获取指定城市的当前天气。
// 流程：先用城市名地理编码得到经纬度，再拉取当前天气。
// 任何一步失败都返回 error，由调用方决定兜底（通常静默忽略天气）。
func Fetch(ctx context.Context, city string) (Weather, error) {
	city = strings.TrimSpace(city)
	if city == "" {
		return Weather{}, fmt.Errorf("city is empty")
	}
	// 地理编码 + 当前天气是两次串行网络请求，且各自内部还有一次重试
	// （每次请求 5s 客户端超时）。给整次抓取一个充足的总超时（12s），
	// 同时尊重调用方 ctx 的取消。注意：不要把调用方那个偏短的 context
	// 直接作为硬上限，否则前一步请求消耗掉预算后，后一步必然超时。
	fetchCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	lat, lon, err := geocode(fetchCtx, city)
	if err != nil {
		return Weather{}, err
	}
	return fetchCurrent(fetchCtx, lat, lon, city)
}

// 中国主要城市经纬度表（WGS-84）。
// 当 Open-Meteo 的 geocoding 对中文地名不识别或超时时，用此表兜底，
// 保证「北京」「上海」等常见城市能直接拿到坐标，不再出现 city not found。
// 键同时支持中文和常见拼音/英文别名。
var cityCoordinates = map[string]geoCoord{
	"北京": {39.9042, 116.4074}, "beijing": {39.9042, 116.4074},
	"上海": {31.2304, 121.4737}, "shanghai": {31.2304, 121.4737},
	"广州": {23.1291, 113.2644}, "guangzhou": {23.1291, 113.2644},
	"深圳": {22.5431, 114.0579}, "shenzhen": {22.5431, 114.0579},
	"厦门": {24.4798, 118.0894}, "xiamen": {24.4798, 118.0894},
	"杭州": {30.2741, 120.1551}, "hangzhou": {30.2741, 120.1551},
	"南京": {32.0603, 118.7969}, "nanjing": {32.0603, 118.7969},
	"成都": {30.5728, 104.0668}, "chengdu": {30.5728, 104.0668},
	"武汉": {30.5928, 114.3055}, "wuhan": {30.5928, 114.3055},
	"西安": {34.3416, 108.9398}, "xian": {34.3416, 108.9398},
	"重庆": {29.5630, 106.5516}, "chongqing": {29.5630, 106.5516},
	"天津": {39.0842, 117.2010}, "tianjin": {39.0842, 117.2010},
	"苏州": {31.2989, 120.5853}, "suzhou": {31.2989, 120.5853},
	"长沙": {28.2280, 112.9388}, "changsha": {28.2280, 112.9388},
	"青岛": {36.0671, 120.3826}, "qingdao": {36.0671, 120.3826},
	"大连": {38.9140, 121.6147}, "dalian": {38.9140, 121.6147},
	"沈阳": {41.8057, 123.4315}, "shenyang": {41.8057, 123.4315},
	"哈尔滨": {45.8038, 126.5350}, "haerbin": {45.8038, 126.5350}, "harbin": {45.8038, 126.5350},
	"济南": {36.6512, 117.1201}, "jinan": {36.6512, 117.1201},
	"郑州": {34.7466, 113.6253}, "zhengzhou": {34.7466, 113.6253},
	"昆明": {25.0389, 102.7183}, "kunming": {25.0389, 102.7183},
	"福州": {26.0745, 119.2965}, "fuzhou": {26.0745, 119.2965},
	"南宁": {22.8170, 108.3665}, "nanning": {22.8170, 108.3665},
	"海口": {20.0440, 110.1999}, "haikou": {20.0440, 110.1999},
	"乌鲁木齐": {43.8256, 87.6168}, "wulumuqi": {43.8256, 87.6168}, "urumqi": {43.8256, 87.6168},
	"兰州": {36.0611, 103.8343}, "lanzhou": {36.0611, 103.8343},
	"银川": {38.4872, 106.2309}, "yinchuan": {38.4872, 106.2309},
	"西宁": {36.6171, 101.7782}, "xining": {36.6171, 101.7782},
	"拉萨": {29.6500, 91.1000}, "lasa": {29.6500, 91.1000}, "lhasa": {29.6500, 91.1000},
	"呼和浩特": {40.8414, 111.7519}, "huhehaote": {40.8414, 111.7519}, "hohhot": {40.8414, 111.7519},
	"石家庄": {38.0428, 114.5149}, "shijiazhuang": {38.0428, 114.5149},
	"太原": {37.8706, 112.5489}, "taiyuan": {37.8706, 112.5489},
	"合肥": {31.8206, 117.2272}, "hefei": {31.8206, 117.2272},
	"南昌": {28.6820, 115.8579}, "nanchang": {28.6820, 115.8579},
	"贵阳": {26.6470, 106.6302}, "guiyang": {26.6470, 106.6302},
	"长春": {43.8171, 125.3235}, "changchun": {43.8171, 125.3235},
	"宁波": {29.8683, 121.5440}, "ningbo": {29.8683, 121.5440},
	"无锡": {31.5686, 120.2986}, "wuxi": {31.5686, 120.2986},
	"佛山": {23.0218, 113.1219}, "foshan": {23.0218, 113.1219},
	"东莞": {23.0207, 113.7518}, "dongguan": {23.0207, 113.7518},
}

type geoCoord struct {
	lat, lon float64
}

// lookupCoord 先查内置城市表（大小写不敏感），命中直接返回；
// 未命中再走 Open-Meteo geocoding，这样中文城市名也能稳定解析。
func lookupCoord(city string) (geoCoord, bool) {
	key := strings.ToLower(strings.TrimSpace(city))
	if key == "" {
		return geoCoord{}, false
	}
	if c, ok := cityCoordinates[key]; ok {
		return c, true
	}
	return geoCoord{}, false
}

func geocode(ctx context.Context, city string) (float64, float64, error) {
	city = strings.TrimSpace(city)
	if city == "" {
		return 0, 0, fmt.Errorf("city is empty")
	}
	// 优先内置表：避免 Open-Meteo geocoding 对中文城市名识别失败。
	if c, ok := lookupCoord(city); ok {
		return c.lat, c.lon, nil
	}
	// 兜底：用 Open-Meteo geocoding 搜索（支持英文/拼音等）。
	api := "https://geocoding-api.open-meteo.com/v1/search?count=1&language=zh&format=json&name=" + url.QueryEscape(city)
	resp, err := getURL(ctx, api)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	var g struct {
		Results []struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Name      string  `json:"name"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		return 0, 0, err
	}
	if len(g.Results) == 0 {
		return 0, 0, fmt.Errorf("city not found: %s", city)
	}
	return g.Results[0].Latitude, g.Results[0].Longitude, nil
}

func fetchCurrent(ctx context.Context, lat, lon float64, city string) (Weather, error) {
	api := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&current_weather=true", lat, lon)
	resp, err := getURL(ctx, api)
	if err != nil {
		return Weather{}, err
	}
	defer resp.Body.Close()
	var f struct {
		CurrentWeather struct {
			Temperature  float64 `json:"temperature"`
			WeatherCode  int     `json:"weathercode"`
			WindSpeed    float64 `json:"windspeed"`
			WindDirection int    `json:"winddirection"`
			IsDay        int     `json:"is_day"`
		} `json:"current_weather"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		return Weather{}, err
	}
	emoji, text := describe(f.CurrentWeather.WeatherCode)
	return Weather{
		City:        city,
		Temperature: f.CurrentWeather.Temperature,
		Code:        f.CurrentWeather.WeatherCode,
		Text:        text,
		Emoji:       emoji,
		IsDay:       f.CurrentWeather.IsDay,
	}, nil
}

// describe 把 WMO weather code 映射为 emoji + 中文文案。
func describe(code int) (string, string) {
	switch code {
	case 0:
		return "☀️", "晴"
	case 1:
		return "🌤️", "晴间多云"
	case 2:
		return "⛅", "局部多云"
	case 3:
		return "☁️", "阴"
	case 45, 48:
		return "🌫️", "雾"
	case 51, 53, 55:
		return "🌦️", "毛毛雨"
	case 56, 57:
		return "🌧️", "冻毛毛雨"
	case 61, 63, 65:
		return "🌧️", "雨"
	case 66, 67:
		return "🌧️", "冻雨"
	case 71, 73, 75:
		return "🌨️", "雪"
	case 77:
		return "🌨️", "雪粒"
	case 80, 81, 82:
		return "🌦️", "阵雨"
	case 85, 86:
		return "🌨️", "阵雪"
	case 95:
		return "⛈️", "雷阵雨"
	case 96, 99:
		return "⛈️", "雷阵雨伴冰雹"
	default:
		return "🌡️", "未知"
	}
}

// TempString 把温度格式化为整数度的字符串（如 "26°C"）。
func TempString(t float64) string {
	return strconv.Itoa(int(math.Round(t))) + "°C"
}
