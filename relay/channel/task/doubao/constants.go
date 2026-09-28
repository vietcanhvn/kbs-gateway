package doubao

import "strings"

var ModelList = []string{
	"doubao-seedance-1-0-pro-250528",
	"doubao-seedance-1-0-lite-t2v",
	"doubao-seedance-1-0-lite-i2v",
	"doubao-seedance-1-5-pro-251215",
	"doubao-seedance-2-0-260128",
	"doubao-seedance-2-0-fast-260128",
}

var ChannelName = "doubao-video"

// videoPriceKey 价格表的键：输出分辨率档（is1080p/is4k 均为 false 即 480p/720p 基准档）、输入是否含视频。
type videoPriceKey struct {
	is1080p  bool
	is4k     bool
	hasVideo bool
}

// videoPriceTable 各模型在不同 (输出分辨率档, 是否含视频输入) 下的单价（元/百万 token）。
// 其中零值键 {480p/720p, 不含视频} 为基准价，等于管理员应配置的 ModelRatio；
// 计费时取 实际单价/基准价 作为 OtherRatio。
var videoPriceTable = map[string]map[videoPriceKey]float64{
	"doubao-seedance-2-0-260128": {
		{hasVideo: false}:                46.0,
		{hasVideo: true}:                 28.0,
		{is1080p: true, hasVideo: false}: 51.0,
		{is1080p: true, hasVideo: true}:  31.0,
		{is4k: true, hasVideo: false}:    26.0,
		{is4k: true, hasVideo: true}:     16.0,
	},
	"doubao-seedance-2-0-fast-260128": {
		{hasVideo: false}: 37.0,
		{hasVideo: true}:  22.0,
	},
}

// BytePlus ModelArk list prices (USD per 1K tokens, 2026-09) by model family.
// Custom gateway names ("byteplus-seedance-2.0", "seedance-2.0-reference",
// "seedance-2.0-fast"...) do not match the Volcengine IDs above, so they are
// matched by family instead; see seedanceFamily. The base key {480p/720p, no
// video input} is the price the admin sets as ModelRatio (USD per 1M tokens / 2).
var byteplusVideoPriceTable = map[string]map[videoPriceKey]float64{
	"seedance-2.0": {
		{hasVideo: false}:                0.007,
		{hasVideo: true}:                 0.0043,
		{is1080p: true, hasVideo: false}: 0.0077,
		{is1080p: true, hasVideo: true}:  0.0047,
		{is4k: true, hasVideo: false}:    0.004,
		{is4k: true, hasVideo: true}:     0.0024,
	},
	"seedance-2.0-fast": {
		{hasVideo: false}: 0.0056,
		{hasVideo: true}:  0.0033,
	},
	"seedance-2.5": {
		{hasVideo: false}:                0.0107,
		{hasVideo: true}:                 0.0064,
		{is1080p: true, hasVideo: false}: 0.0117,
		{is1080p: true, hasVideo: true}:  0.007,
	},
}

// seedanceFamily maps a model name to a BytePlus price family, or "".
func seedanceFamily(modelName string) string {
	name := strings.ToLower(strings.TrimSpace(modelName))
	if !strings.Contains(name, "seedance") {
		return ""
	}
	name = strings.NewReplacer("2-0", "2.0", "2-5", "2.5", "2_0", "2.0", "2_5", "2.5").Replace(name)
	switch {
	case strings.Contains(name, "2.5"):
		return "seedance-2.5"
	case strings.Contains(name, "2.0") && strings.Contains(name, "fast"):
		return "seedance-2.0-fast"
	case strings.Contains(name, "2.0"):
		return "seedance-2.0"
	default:
		return ""
	}
}

// videoPricesFor returns the price table for a model: exact Volcengine ID
// first, then the BytePlus family.
func videoPricesFor(modelName string) (map[videoPriceKey]float64, bool) {
	if prices, ok := videoPriceTable[modelName]; ok {
		return prices, true
	}
	prices, ok := byteplusVideoPriceTable[seedanceFamily(modelName)]
	return prices, ok
}

// GetVideoInputRatio 返回指定模型在给定输出分辨率/是否含视频输入下，相对基准价的计费倍率。
// 第二个返回值表示该模型是否配置了价格表；倍率为 1.0 时调用方可忽略该 OtherRatio。
func GetVideoInputRatio(modelName, resolution string, hasVideo bool) (float64, bool) {
	prices, ok := videoPricesFor(modelName)
	base := prices[videoPriceKey{}] // 零值键 = {480p/720p, 不含视频} 基准价
	if !ok || base <= 0 {
		return 0, false
	}
	res := strings.ToLower(strings.TrimSpace(resolution))
	price, ok := prices[videoPriceKey{is1080p: res == "1080p", is4k: res == "4k", hasVideo: hasVideo}]
	if !ok {
		// 未配置的组合（如 fast 无 1080p/4k，上游会自行报错）按基准价计费即可。
		return 1.0, true
	}
	return price / base, true
}
