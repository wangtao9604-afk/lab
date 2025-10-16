package keywords

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// TravelMode 表示用户描述的出行方式。
type TravelMode string

const (
	ModeWalk  TravelMode = "walk"
	ModeBike  TravelMode = "bike"
	ModeDrive TravelMode = "drive"
)

// TravelSpeeds 保存不同出行方式下的速度（米/分钟）。
type TravelSpeeds struct {
	WalkMpm  float64
	BikeMpm  float64
	DriveMpm float64
}

// SelectPolicy 用于后续若需要在多个约束中选择最优时提供偏好。
type SelectPolicy struct {
	PreferredAnchor string
	ModeOrder       []TravelMode
}

// DurationRange 表示分钟范围及原始文案。
type DurationRange struct {
	MinMinutes float64 `json:"min_minutes"`
	MaxMinutes float64 `json:"max_minutes"`
	Raw        string  `json:"raw"`
}

// TravelConstraint 是解析出来的核心结构。
type TravelConstraint struct {
	AnchorRaw       string        `json:"anchor_raw"`
	AnchorType      string        `json:"anchor_type"`
	Mode            TravelMode    `json:"mode"`
	Duration        DurationRange `json:"duration"`
	RadiusMinMeters float64       `json:"radius_min_m,omitempty"`
	RadiusMaxMeters float64       `json:"radius_max_m,omitempty"`
}

var (
	defaultModeOrder = []TravelMode{ModeWalk, ModeBike, ModeDrive}

	reModeAfterAnchor  *regexp.Regexp
	reModeBeforeAnchor *regexp.Regexp
	reRange            *regexp.Regexp
	reSingle           *regexp.Regexp

	reMetro *regexp.Regexp
	reBus   *regexp.Regexp
	reAddr  *regexp.Regexp
)

func init() {
	reModeAfterAnchor = regexp.MustCompile(`(?i)(?:离|距|距离|到|至|去|从)\s*([^，。！？；\s]{1,30}?)\s*(步行|走路|甩火腿|骑(?:单车|共享单车|车|电瓶车|电马儿|自行车)|自行车|驾车|开车)\s*(?:大约|大概|约|大致|大概在|大概是|大概有|大约在)?\s*([0-9]+(?:\.[0-9]+)?(?:\s*(?:分钟|小时|min|h|hr|hrs))|半小时|一刻钟|三刻钟|[0-9]+(?:\s*[-~～—到至]\s*[0-9]+(?:\.[0-9]+)?)\s*(?:分钟|小时|min|h|hr|hrs))`)
	reModeBeforeAnchor = regexp.MustCompile(`(?i)(步行|走路|甩火腿|骑(?:单车|共享单车|车|电瓶车|电马儿|自行车)|自行车|驾车|开车)\s*(?:到|至|去)\s*([^，。！？；\s]{1,30}?)\s*(?:大约|大概|约|大致|大概在|大概是|大概有|大约在)?\s*([0-9]+(?:\.[0-9]+)?(?:\s*(?:分钟|小时|min|h|hr|hrs))|半小时|一刻钟|三刻钟|[0-9]+(?:\s*[-~～—到至]\s*[0-9]+(?:\.[0-9]+)?)\s*(?:分钟|小时|min|h|hr|hrs))`)
	reRange = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)(?:\s*[-~～—到至]\s*)([0-9]+(?:\.[0-9]+)?)(分钟|小时|min|h|hr|hrs)`)
	reSingle = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)(分钟|小时|min|h|hr|hrs)`)

	reMetro = regexp.MustCompile(`(?i)(?:地铁.{0,6}|轨交.{0,6}|[0-9一二三四五六七八九十]+号线)[^，。；\s]{0,10}站$`)
	reBus = regexp.MustCompile(`(?i)(?:公交|巴士|[0-9]{2,4}路)[^，。；\s]{0,8}站$`)
	reAddr = regexp.MustCompile(`(?i)(路|街|道|弄|号|村|镇|苑|里|坊|桥|支路|公路|大道)`)
}

// ParseTravelConstraints 解析文本中的所有出行约束。
// speeds 若任一值 <=0，则回退到默认速度。
func (afe *AdvancedFieldExtractor) ParseTravelConstraints(text string, speeds TravelSpeeds) []TravelConstraint {
	return parseTravelConstraintsInternal(text, normalizeSpeeds(speeds))
}

func normalizeSpeeds(s TravelSpeeds) TravelSpeeds {
	res := s
	if res.WalkMpm <= 0 {
		res.WalkMpm = 80
	}
	if res.BikeMpm <= 0 {
		res.BikeMpm = 230
	}
	if res.DriveMpm <= 0 {
		res.DriveMpm = 500
	}
	return res
}

func parseTravelConstraintsInternal(text string, speeds TravelSpeeds) []TravelConstraint {
	txt := strings.TrimSpace(text)
	if txt == "" {
		return nil
	}
	var constraints []TravelConstraint
	collect := func(anchor, modeStr, durationRaw string) {
		mode := normalizeMode(modeStr)
		if mode == "" {
			return
		}
		dur, ok := parseDuration(durationRaw)
		if !ok {
			return
		}
		rmin, rmax := durationToRadius(dur, mode, speeds)
		constraints = append(constraints, TravelConstraint{
			AnchorRaw:       strings.TrimSpace(anchor),
			AnchorType:      detectAnchorType(anchor),
			Mode:            mode,
			Duration:        dur,
			RadiusMinMeters: rmin,
			RadiusMaxMeters: rmax,
		})
	}

	for _, match := range reModeAfterAnchor.FindAllStringSubmatch(txt, -1) {
		if len(match) < 4 {
			continue
		}
		collect(match[1], match[2], match[3])
	}
	for _, match := range reModeBeforeAnchor.FindAllStringSubmatch(txt, -1) {
		if len(match) < 4 {
			continue
		}
		collect(match[2], match[1], match[3])
	}
	return constraints
}

func normalizeMode(raw string) TravelMode {
	s := strings.ToLower(raw)
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\t", "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")

	if strings.Contains(s, "步") || strings.Contains(s, "走路") || strings.Contains(s, "甩火腿") {
		return ModeWalk
	}
	if strings.Contains(s, "驾") || strings.Contains(s, "开车") {
		return ModeDrive
	}
	if strings.Contains(s, "骑") || strings.Contains(s, "自行车") {
		return ModeBike
	}
	return ""
}

func detectAnchorType(anchor string) string {
	trimmed := strings.TrimSpace(anchor)
	if trimmed == "" {
		return "unknown"
	}
	if reMetro.MatchString(trimmed) {
		return "metro_station"
	}
	if reBus.MatchString(trimmed) {
		return "bus_stop"
	}
	if reAddr.MatchString(trimmed) {
		return "street_address"
	}
	return "poi"
}

func parseDuration(raw string) (DurationRange, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return DurationRange{}, false
	}
	lower := strings.ToLower(text)
	switch lower {
	case "半小时":
		return DurationRange{MinMinutes: 30, MaxMinutes: 30, Raw: raw}, true
	case "一刻钟":
		return DurationRange{MinMinutes: 15, MaxMinutes: 15, Raw: raw}, true
	case "三刻钟":
		return DurationRange{MinMinutes: 45, MaxMinutes: 45, Raw: raw}, true
	}

	if groups := reRange.FindStringSubmatch(lower); len(groups) == 4 {
		min, _ := strconv.ParseFloat(groups[1], 64)
		max, _ := strconv.ParseFloat(groups[2], 64)
		min = UnitToMinutes(min, groups[3])
		max = UnitToMinutes(max, groups[3])
		if min <= 0 || max <= 0 {
			return DurationRange{}, false
		}
		if min > max {
			min, max = max, min
		}
		return DurationRange{MinMinutes: min, MaxMinutes: max, Raw: raw}, true
	}

	if groups := reSingle.FindStringSubmatch(lower); len(groups) == 3 {
		val, err := strconv.ParseFloat(groups[1], 64)
		if err != nil {
			return DurationRange{}, false
		}
		minutes := UnitToMinutes(val, groups[2])
		if minutes <= 0 {
			return DurationRange{}, false
		}
		return DurationRange{MinMinutes: minutes, MaxMinutes: minutes, Raw: raw}, true
	}

	return DurationRange{}, false
}

// UnitToMinutes 将数值与单位转换为分钟。
func UnitToMinutes(value float64, unit string) float64 {
	u := strings.ToLower(strings.TrimSpace(unit))
	switch u {
	case "小时", "h", "hr", "hrs":
		return value * 60
	default:
		return value
	}
}

func durationToRadius(d DurationRange, mode TravelMode, speeds TravelSpeeds) (float64, float64) {
	speed := speeds.WalkMpm
	switch mode {
	case ModeBike:
		speed = speeds.BikeMpm
	case ModeDrive:
		speed = speeds.DriveMpm
	}
	minMeters := d.MinMinutes * speed
	maxMeters := d.MaxMinutes * speed
	if minMeters < 0 {
		minMeters = 0
	}
	if maxMeters < minMeters {
		maxMeters = minMeters
	}
	return roundMeters(minMeters), roundMeters(maxMeters)
}

func roundMeters(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return math.Round(v*10) / 10 // 保留一位小数，便于展示
}
