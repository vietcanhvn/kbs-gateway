package mediaworkflow

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
)

// InputMapping is the stored per-workflow binding of request fields to
// workflow node inputs.
type InputMapping struct {
	Inputs []InputBinding `json:"inputs"`
	// Billing is "per_call" (default: the model price is charged once) or
	// "per_second" (the model price is multiplied by the requested seconds).
	Billing string `json:"billing,omitempty"`
	// InstanceType is passed to RunningHub ("plus" = 48 GB GPU).
	InstanceType string `json:"instance_type,omitempty"`
	// UnusedMedia says what happens to an optional image/video/audio slot
	// the request leaves empty. "" (default) unwires the loader from the
	// nodes it feeds, so the sample file saved in the workflow is not used;
	// "keep" leaves the workflow's own file in place.
	UnusedMedia string `json:"unused_media,omitempty"`
}

// InputBinding binds one request role to one node input.
//
//   - prompt / negative_prompt: text after the prompt template is applied
//   - image / video / audio: the Index-th media of that kind (0-based)
//   - last_frame: metadata.last_frame_image
//   - duration: seconds, clamped to Min/Max, written as int or float
//   - aspect_ratio: request ratio translated through Enum
//   - width / height / seed: numbers (seed is random when not requested)
//   - fixed: always writes Value
type InputBinding struct {
	Role      string            `json:"role"`
	Index     int               `json:"index,omitempty"`
	NodeID    string            `json:"node_id"`
	Field     string            `json:"field"`
	Required  bool              `json:"required,omitempty"`
	Min       *float64          `json:"min,omitempty"`
	Max       *float64          `json:"max,omitempty"`
	ValueType string            `json:"value_type,omitempty"`
	Enum      map[string]string `json:"enum,omitempty"`
	Value     any               `json:"value,omitempty"`
}

// NodeOverride is one entry of RunningHub's nodeInfoList (and what a
// self-hosted ComfyUI executor writes into the API JSON).
type NodeOverride struct {
	NodeID     string `json:"nodeId"`
	FieldName  string `json:"fieldName"`
	FieldValue any    `json:"fieldValue"`
}

// MediaRequest is a DC-Media request reduced to what workflow inputs need.
// Media fields hold the executor's file references (already uploaded).
type MediaRequest struct {
	Prompt         string
	NegativePrompt string
	Images         []string
	Videos         []string
	Audios         []string
	LastFrame      string
	Duration       int
	Width          int
	Height         int
	Ratio          string
	// Resolution tier ("720p", "1080p"...) for megapixels / long_edge inputs.
	Resolution string
	Seed       int64
}

// RequestError marks a problem with the client's request (HTTP 400) as
// opposed to a workflow configuration problem.
type RequestError struct{ Message string }

func (e *RequestError) Error() string { return e.Message }

func requestErrorf(format string, args ...any) error {
	return &RequestError{Message: fmt.Sprintf(format, args...)}
}

// HasRole reports whether any input is bound to the role.
func (m InputMapping) HasRole(role string) bool {
	for _, binding := range m.Inputs {
		if binding.Role == role {
			return true
		}
	}
	return false
}

// MediaCapacity reports how many media of each kind a mapping accepts.
func (m InputMapping) MediaCapacity() map[string]int {
	capacity := map[string]int{}
	if m.HasRole(RoleTimeline) {
		// Timeline workflows place media per segment (ComfyUI-Easy-Media).
		capacity[RoleImage] = 24
		capacity[RoleVideo] = 6
		capacity[RoleAudio] = 6
	}
	for _, binding := range m.Inputs {
		switch binding.Role {
		case RoleImage, RoleVideo, RoleAudio:
			if binding.Index+1 > capacity[binding.Role] {
				capacity[binding.Role] = binding.Index + 1
			}
		case RoleLastFrame:
			capacity[RoleLastFrame] = 1
		}
	}
	return capacity
}

// UnwireUnusedMedia returns overrides that disconnect the loaders of
// optional media slots (and an optional last frame) the request leaves empty: every input that links to
// such a loader is set to null. A workflow can then expose many reference
// slots (one RunningHub workflow ID) and still run with fewer files without
// its saved sample files leaking into the result.
// passThroughMediaInputs are input names through which a helper node takes
// the file it transforms; a node fed an unused file through one of them has
// nothing to work on.
var passThroughMediaInputs = map[string]bool{"image": true, "images": true, "video": true, "audio": true, "pixels": true}

func UnwireUnusedMedia(mapping InputMapping, workflow map[string]map[string]any, req MediaRequest) []NodeOverride {
	if strings.EqualFold(strings.TrimSpace(mapping.UnusedMedia), "keep") || len(workflow) == 0 {
		return nil
	}
	counts := map[string]int{RoleImage: len(req.Images), RoleVideo: len(req.Videos), RoleAudio: len(req.Audios)}
	unused := map[string]bool{}
	for _, binding := range mapping.Inputs {
		switch binding.Role {
		case RoleImage, RoleVideo, RoleAudio:
			if binding.Index >= counts[binding.Role] && !binding.Required {
				unused[binding.NodeID] = true
			}
		case RoleLastFrame:
			// One workflow can do first frame and first + last frame.
			if strings.TrimSpace(req.LastFrame) == "" && !binding.Required {
				unused[binding.NodeID] = true
			}
		}
	}
	if len(unused) == 0 {
		return nil
	}
	// A node that only processes an unused file (resize, crop...) is unused
	// too: unwire at the node where the file would have joined the rest of the
	// workflow, not at the helper that would then miss its input.
	for changed := true; changed; {
		changed = false
		for _, id := range sortedNodeIDs(workflow) {
			if unused[id] {
				continue
			}
			inputs, _ := workflow[id]["inputs"].(map[string]any)
			for field, value := range inputs {
				link, ok := value.([]any)
				if ok && len(link) == 2 && unused[scalarString(link[0])] && passThroughMediaInputs[strings.ToLower(field)] {
					unused[id] = true
					changed = true
					break
				}
			}
		}
	}
	overrides := make([]NodeOverride, 0)
	for _, id := range sortedNodeIDs(workflow) {
		if unused[id] {
			continue
		}
		inputs, _ := workflow[id]["inputs"].(map[string]any)
		for _, field := range sortedKeys(inputs) {
			link, ok := inputs[field].([]any)
			if !ok || len(link) != 2 || !unused[scalarString(link[0])] {
				continue
			}
			overrides = append(overrides, NodeOverride{NodeID: id, FieldName: field, FieldValue: nil})
		}
	}
	return overrides
}

// Validate checks the mapping itself (not a request) and, when the workflow
// is given, that every bound node exists.
func (m InputMapping) Validate(workflow map[string]map[string]any) error {
	seen := map[string]bool{}
	for _, binding := range m.Inputs {
		if strings.TrimSpace(binding.NodeID) == "" || strings.TrimSpace(binding.Field) == "" {
			return fmt.Errorf("input %q needs a node and a field", binding.Role)
		}
		if roleOrder(binding.Role) == roleOrder("unknown") && binding.Role != RoleFixed {
			return fmt.Errorf("unknown input role %q", binding.Role)
		}
		if binding.Index < 0 {
			return fmt.Errorf("input %q has a negative index", binding.Role)
		}
		key := binding.NodeID + "." + binding.Field
		if seen[key] {
			return fmt.Errorf("node %s field %s is bound twice", binding.NodeID, binding.Field)
		}
		seen[key] = true
		if workflow != nil {
			if _, ok := workflow[binding.NodeID]; !ok {
				return fmt.Errorf("input %q points to node %s, which is not an active node of the workflow", binding.Role, binding.NodeID)
			}
		}
	}
	if m.Billing != "" && m.Billing != "per_call" && m.Billing != "per_second" {
		return fmt.Errorf("billing must be per_call or per_second")
	}
	return nil
}

// BuildNodeOverrides resolves a request against the mapping. Prompt must
// already be rendered through the workflow's PromptTemplate.
func BuildNodeOverrides(mapping InputMapping, req MediaRequest) ([]NodeOverride, error) {
	capacity := mapping.MediaCapacity()
	for kind, items := range map[string][]string{RoleImage: req.Images, RoleVideo: req.Videos, RoleAudio: req.Audios} {
		if len(items) > capacity[kind] {
			return nil, requestErrorf("this workflow accepts at most %d %s input(s), got %d", capacity[kind], kind, len(items))
		}
	}
	if req.LastFrame != "" && capacity[RoleLastFrame] == 0 {
		return nil, requestErrorf("this workflow has no last-frame input")
	}
	if strings.TrimSpace(req.Prompt) != "" && !mappingHasRole(mapping, RolePrompt) && !mappingHasRole(mapping, RoleTimeline) {
		return nil, fmt.Errorf("workflow has no prompt input mapped")
	}

	overrides := make([]NodeOverride, 0, len(mapping.Inputs))
	set := func(binding InputBinding, value any) {
		overrides = append(overrides, NodeOverride{NodeID: binding.NodeID, FieldName: binding.Field, FieldValue: value})
	}
	for _, binding := range mapping.Inputs {
		switch binding.Role {
		case RolePrompt, RoleNegativePrompt:
			text := req.Prompt
			if binding.Role == RoleNegativePrompt {
				text = req.NegativePrompt
			}
			if strings.TrimSpace(text) == "" {
				if binding.Required {
					return nil, requestErrorf("%s is required", binding.Role)
				}
				continue
			}
			set(binding, text)
		case RoleImage, RoleVideo, RoleAudio:
			items := map[string][]string{RoleImage: req.Images, RoleVideo: req.Videos, RoleAudio: req.Audios}[binding.Role]
			if binding.Index >= len(items) {
				if binding.Required {
					return nil, requestErrorf("%s %d is required by this workflow", binding.Role, binding.Index+1)
				}
				continue
			}
			set(binding, items[binding.Index])
		case RoleLastFrame:
			if req.LastFrame == "" {
				if binding.Required {
					return nil, requestErrorf("last_frame_image is required by this workflow")
				}
				continue
			}
			set(binding, req.LastFrame)
		case RoleDuration:
			if req.Duration <= 0 {
				continue
			}
			set(binding, numberForBinding(binding, clamp(float64(req.Duration), binding.Min, binding.Max)))
		case RoleAspectRatio:
			if value, ok := aspectRatioValue(binding, req); ok {
				set(binding, value)
			}
		case RoleWidth:
			if req.Width > 0 {
				set(binding, numberForBinding(binding, clamp(float64(req.Width), binding.Min, binding.Max)))
			}
		case RoleHeight:
			if req.Height > 0 {
				set(binding, numberForBinding(binding, clamp(float64(req.Height), binding.Min, binding.Max)))
			}
		case RoleMegapixels, RoleLongEdge:
			if value, ok := resolutionValue(binding, req); ok {
				set(binding, value)
			}
		case RoleSeed:
			seed := req.Seed
			if seed <= 0 {
				// RunningHub re-randomizes seeds only for some nodes; send
				// one explicitly so every run differs and stays traceable.
				seed = rand.Int64N(1 << 48)
			}
			set(binding, seed)
		case RoleFixed:
			set(binding, binding.Value)
		}
	}
	return overrides, nil
}

func mappingHasRole(mapping InputMapping, role string) bool {
	for _, binding := range mapping.Inputs {
		if binding.Role == role {
			return true
		}
	}
	return false
}

func clamp(value float64, minValue, maxValue *float64) float64 {
	if minValue != nil && value < *minValue {
		value = *minValue
	}
	if maxValue != nil && value > *maxValue {
		value = *maxValue
	}
	return value
}

func numberForBinding(binding InputBinding, value float64) any {
	if binding.ValueType == "float" {
		return value
	}
	return int64(math.Round(value))
}

// RequestRatio returns the aspect ratio a request asks for: the explicit
// metadata ratio, else the reduced width:height. "" means none / auto.
func RequestRatio(ratio string, width, height int) string {
	ratio = strings.ToLower(strings.TrimSpace(ratio))
	if ratio == "auto" || ratio == "adaptive" {
		return ""
	}
	if ratio != "" {
		return ratio
	}
	if width <= 0 || height <= 0 {
		return ""
	}
	divisor := gcd(width, height)
	return fmt.Sprintf("%d:%d", width/divisor, height/divisor)
}

// ParseSize reads "1280x720" (also "1280*720").
func ParseSize(size string) (int, int) {
	size = strings.ToLower(strings.TrimSpace(size))
	for _, separator := range []string{"x", "*", "×"} {
		parts := strings.Split(size, separator)
		if len(parts) != 2 {
			continue
		}
		width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
		height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
		if widthErr == nil && heightErr == nil && width > 0 && height > 0 {
			return width, height
		}
	}
	return 0, 0
}

// aspectRatioValue maps the requested ratio through the binding's Enum. An
// exact key wins; otherwise the numerically closest key is used so a
// 1280x720 request still lands on "16:9 (...)". Without an Enum the ratio is
// passed through as "W:H".
func aspectRatioValue(binding InputBinding, req MediaRequest) (string, bool) {
	ratio := RequestRatio(req.Ratio, req.Width, req.Height)
	if ratio == "" {
		return "", false
	}
	if len(binding.Enum) == 0 {
		return ratio, true
	}
	if value, ok := binding.Enum[ratio]; ok {
		return value, true
	}
	want, ok := ratioNumber(ratio)
	if !ok {
		return "", false
	}
	keys := make([]string, 0, len(binding.Enum))
	for key := range binding.Enum {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	best, bestDistance := "", math.Inf(1)
	for _, key := range keys {
		value, ok := ratioNumber(key)
		if !ok {
			continue
		}
		distance := math.Abs(math.Log(value / want))
		if distance < bestDistance {
			best, bestDistance = key, distance
		}
	}
	if best == "" {
		return "", false
	}
	return binding.Enum[best], true
}

func ratioNumber(ratio string) (float64, bool) {
	parts := strings.Split(ratio, ":")
	if len(parts) != 2 {
		return 0, false
	}
	width, widthErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	height, heightErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return 0, false
	}
	return width / height, true
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
