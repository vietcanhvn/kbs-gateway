// Package mediaworkflow holds the executor-independent part of the media
// workflow registry: reading a ComfyUI workflow export, suggesting how DC-Media
// request fields map onto its nodes, rendering prompts and building the list
// of node overrides an executor (RunningHub today, a self-hosted ComfyUI
// later) applies before running it.
package mediaworkflow

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Media kinds and input roles understood by the registry.
const (
	KindImage = "image"
	KindVideo = "video"
	KindAudio = "audio"

	RolePrompt         = "prompt"
	RoleNegativePrompt = "negative_prompt"
	RoleImage          = "image"
	RoleVideo          = "video"
	RoleAudio          = "audio"
	RoleLastFrame      = "last_frame"
	RoleDuration       = "duration"
	RoleAspectRatio    = "aspect_ratio"
	RoleWidth          = "width"
	RoleHeight         = "height"
	RoleSeed           = "seed"
	RoleFixed          = "fixed"
	// RoleText marks extra text nodes the analyzer found but did not pick as
	// the main prompt. They are listed so the admin can switch the binding.
	RoleText = "text"
)

// Node modes from the ComfyUI editor ("full") export.
const (
	nodeModeMuted    = 2
	nodeModeBypassed = 4
)

type NodeInfo struct {
	ID        string `json:"id"`
	ClassType string `json:"class_type"`
	Title     string `json:"title,omitempty"`
	// State is "active" (runs), "bypassed" / "muted" (disabled in the editor)
	// or "virtual" (editor-only helper such as notes, Get/Set or reroutes).
	State   string `json:"state"`
	Package string `json:"package,omitempty"`
}

type ModelFile struct {
	NodeID    string `json:"node_id"`
	ClassType string `json:"class_type"`
	Field     string `json:"field"`
	Name      string `json:"name"`
	Active    bool   `json:"active"`
}

type PackageInfo struct {
	Name      string   `json:"name"`
	NodeTypes []string `json:"node_types"`
	Active    bool     `json:"active"`
}

type InputCandidate struct {
	Role         string `json:"role"`
	Index        int    `json:"index"`
	NodeID       string `json:"node_id"`
	ClassType    string `json:"class_type"`
	Title        string `json:"title,omitempty"`
	Field        string `json:"field"`
	CurrentValue any    `json:"current_value,omitempty"`
	Active       bool   `json:"active"`
}

type OutputCandidate struct {
	NodeID    string `json:"node_id"`
	ClassType string `json:"class_type"`
	Title     string `json:"title,omitempty"`
	Kind      string `json:"kind"`
	Active    bool   `json:"active"`
}

type Analysis struct {
	ActiveNodes   int               `json:"active_nodes"`
	DisabledNodes int               `json:"disabled_nodes"`
	EditorNodes   int               `json:"editor_nodes,omitempty"`
	MediaKind     string            `json:"media_kind,omitempty"`
	Nodes         []NodeInfo        `json:"nodes"`
	Models        []ModelFile       `json:"models"`
	Packages      []PackageInfo     `json:"packages"`
	Inputs        []InputCandidate  `json:"inputs"`
	Outputs       []OutputCandidate `json:"outputs"`
	Warnings      []string          `json:"warnings,omitempty"`
}

// ParseAPIWorkflow decodes an "Export (API)" workflow: an object keyed by node
// ID whose values carry class_type and inputs. RunningHub's
// getJsonApiFormat returns the same JSON wrapped in a string, which is also
// accepted.
func ParseAPIWorkflow(raw []byte) (map[string]map[string]any, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return nil, fmt.Errorf("workflow API JSON is empty")
	}
	if strings.HasPrefix(text, "\"") {
		var inner string
		if err := common.Unmarshal([]byte(text), &inner); err != nil {
			return nil, fmt.Errorf("workflow API JSON: %w", err)
		}
		text = inner
	}
	var decoded map[string]any
	if err := common.Unmarshal([]byte(text), &decoded); err != nil {
		return nil, fmt.Errorf("workflow API JSON: %w", err)
	}
	if prompt, ok := decoded["prompt"].(map[string]any); ok && decoded["class_type"] == nil {
		decoded = prompt
	}
	if _, isEditorExport := decoded["nodes"].([]any); isEditorExport {
		return nil, fmt.Errorf("this is the editor workflow JSON; export the workflow with \"Export (API)\" for the API JSON")
	}
	workflow := make(map[string]map[string]any, len(decoded))
	for id, value := range decoded {
		node, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := node["class_type"].(string); !ok {
			continue
		}
		workflow[id] = node
	}
	if len(workflow) == 0 {
		return nil, fmt.Errorf("workflow API JSON has no nodes with class_type")
	}
	return workflow, nil
}

type editorNode struct {
	ID         string
	Type       string
	Title      string
	Mode       int
	Widgets    any
	Properties map[string]any
}

func parseEditorWorkflow(raw []byte) ([]editorNode, error) {
	var decoded struct {
		Nodes       []map[string]any `json:"nodes"`
		Definitions struct {
			Subgraphs []struct {
				Nodes []map[string]any `json:"nodes"`
			} `json:"subgraphs"`
		} `json:"definitions"`
	}
	if err := common.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("workflow editor JSON: %w", err)
	}
	if len(decoded.Nodes) == 0 {
		return nil, fmt.Errorf("workflow editor JSON has no nodes")
	}
	all := decoded.Nodes
	for _, subgraph := range decoded.Definitions.Subgraphs {
		all = append(all, subgraph.Nodes...)
	}
	nodes := make([]editorNode, 0, len(all))
	for _, item := range all {
		node := editorNode{
			ID:      scalarString(item["id"]),
			Type:    scalarString(item["type"]),
			Title:   scalarString(item["title"]),
			Widgets: item["widgets_values"],
		}
		if mode, ok := item["mode"].(float64); ok {
			node.Mode = int(mode)
		}
		node.Properties, _ = item["properties"].(map[string]any)
		if node.ID == "" || node.Type == "" {
			continue
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// Analyze inspects an API-format workflow and, when given, the editor export
// of the same workflow. The API export only contains nodes that run; the
// editor export adds bypassed / muted nodes (reported as available but
// disabled) and the custom-node package of every node.
func Analyze(apiJSON, editorJSON []byte) (*Analysis, error) {
	workflow, err := ParseAPIWorkflow(apiJSON)
	if err != nil {
		return nil, err
	}
	var editorNodes []editorNode
	if len(strings.TrimSpace(string(editorJSON))) > 0 {
		editorNodes, err = parseEditorWorkflow(editorJSON)
		if err != nil {
			return nil, err
		}
	}
	editorByID := make(map[string]editorNode, len(editorNodes))
	for _, node := range editorNodes {
		editorByID[node.ID] = node
	}

	analysis := &Analysis{ActiveNodes: len(workflow), EditorNodes: len(editorNodes)}
	ids := sortedNodeIDs(workflow)
	packages := map[string]*PackageInfo{}
	for _, id := range ids {
		node := workflow[id]
		classType := scalarString(node["class_type"])
		pkg := editorPackage(editorByID[id])
		analysis.Nodes = append(analysis.Nodes, NodeInfo{ID: id, ClassType: classType, Title: apiNodeTitle(node), State: "active", Package: pkg})
		addPackage(packages, pkg, classType, true)
		inputs, _ := node["inputs"].(map[string]any)
		for _, field := range sortedKeys(inputs) {
			if name, ok := inputs[field].(string); ok && isModelFileName(name) {
				analysis.Models = append(analysis.Models, ModelFile{NodeID: id, ClassType: classType, Field: field, Name: name, Active: true})
			}
		}
	}
	for _, node := range editorNodes {
		if _, active := workflow[node.ID]; active {
			continue
		}
		state := "virtual"
		switch node.Mode {
		case nodeModeBypassed:
			state = "bypassed"
		case nodeModeMuted:
			state = "muted"
		}
		if state == "virtual" {
			continue
		}
		analysis.DisabledNodes++
		pkg := editorPackage(node)
		analysis.Nodes = append(analysis.Nodes, NodeInfo{ID: node.ID, ClassType: node.Type, Title: node.Title, State: state, Package: pkg})
		addPackage(packages, pkg, node.Type, false)
		for _, name := range widgetModelFiles(node.Widgets) {
			analysis.Models = append(analysis.Models, ModelFile{NodeID: node.ID, ClassType: node.Type, Name: name})
		}
	}
	for _, name := range sortedKeys(packages) {
		analysis.Packages = append(analysis.Packages, *packages[name])
	}

	analysis.Inputs = suggestActiveInputs(workflow, ids)
	analysis.Inputs = append(analysis.Inputs, disabledMediaInputs(editorNodes, workflow)...)
	analysis.Outputs = findOutputs(workflow, ids, editorNodes)
	analysis.MediaKind = mediaKindFromOutputs(analysis.Outputs)

	if analysis.MediaKind == "" {
		analysis.Warnings = append(analysis.Warnings, "no output node (SaveImage, VHS_VideoCombine, SaveVideo, SaveAudio...) was found among the active nodes")
	}
	if !hasRole(analysis.Inputs, RolePrompt, true) {
		analysis.Warnings = append(analysis.Warnings, "no prompt text node was found; requests with a prompt will be rejected until a prompt input is mapped")
	}
	if hasPackage(analysis.Packages, "comfyui-workflow-encrypt") {
		analysis.Warnings = append(analysis.Warnings, "some nodes are packed by RunningHub's comfyui-workflow-encrypt; they run on RunningHub but their source package is not named in the export")
	}
	analysis.Normalize()
	return analysis, nil
}

// Normalize turns nil lists into empty ones so the JSON never carries
// null where the admin page expects an array (a workflow without custom
// node packages used to crash the edit sheet).
func (a *Analysis) Normalize() {
	if a.Nodes == nil {
		a.Nodes = []NodeInfo{}
	}
	if a.Models == nil {
		a.Models = []ModelFile{}
	}
	if a.Packages == nil {
		a.Packages = []PackageInfo{}
	}
	if a.Inputs == nil {
		a.Inputs = []InputCandidate{}
	}
	if a.Outputs == nil {
		a.Outputs = []OutputCandidate{}
	}
}

// SuggestMapping turns the active input candidates into an input mapping the
// admin can edit. Only the first prompt/negative/duration/... candidate is
// bound; every image/video/audio loader is bound in order.
func SuggestMapping(analysis *Analysis) InputMapping {
	mapping := InputMapping{}
	if analysis == nil {
		return mapping
	}
	taken := map[string]bool{}
	for _, candidate := range analysis.Inputs {
		if !candidate.Active || candidate.Role == RoleText {
			continue
		}
		single := candidate.Role != RoleImage && candidate.Role != RoleVideo && candidate.Role != RoleAudio
		if single && taken[candidate.Role] {
			continue
		}
		taken[candidate.Role] = true
		binding := InputBinding{Role: candidate.Role, Index: candidate.Index, NodeID: candidate.NodeID, Field: candidate.Field}
		switch candidate.Role {
		case RoleDuration:
			binding.ValueType = numberValueType(candidate.ClassType, candidate.CurrentValue)
		case RoleAspectRatio:
			binding.Enum = defaultAspectRatioEnum(candidate.ClassType, candidate.CurrentValue)
		}
		mapping.Inputs = append(mapping.Inputs, binding)
	}
	return mapping
}

// SuggestOutputNodes returns the active output nodes of the suggested media kind.
func SuggestOutputNodes(analysis *Analysis) []string {
	if analysis == nil {
		return nil
	}
	nodes := make([]string, 0)
	for _, output := range analysis.Outputs {
		if output.Active && output.Kind == analysis.MediaKind {
			nodes = append(nodes, output.NodeID)
		}
	}
	return nodes
}

func suggestActiveInputs(workflow map[string]map[string]any, ids []string) []InputCandidate {
	consumers := buildConsumers(workflow)
	candidates := make([]InputCandidate, 0)
	counts := map[string]int{}
	add := func(role, id, field string, value any) {
		node := workflow[id]
		candidate := InputCandidate{
			Role: role, NodeID: id, ClassType: scalarString(node["class_type"]),
			Title: apiNodeTitle(node), Field: field, CurrentValue: shortValue(value), Active: true,
		}
		if role == RoleImage || role == RoleVideo || role == RoleAudio {
			candidate.Index = counts[role]
			counts[role]++
		}
		candidates = append(candidates, candidate)
	}

	type textCandidate struct {
		id, field string
		value     string
		positive  bool
	}
	texts := make([]textCandidate, 0)
	for _, id := range ids {
		node := workflow[id]
		classType := scalarString(node["class_type"])
		lowerClass := strings.ToLower(classType)
		title := strings.ToLower(apiNodeTitle(node))
		inputs, _ := node["inputs"].(map[string]any)

		if role := loaderRole(lowerClass); role != "" {
			if field := loaderField(role, inputs); field != "" {
				add(role, id, field, inputs[field])
				continue
			}
		}
		if field, value, ok := textField(lowerClass, inputs); ok {
			reach := downstreamInputNames(consumers, id)
			if reach["negative"] || strings.Contains(title, "negative") {
				add(RoleNegativePrompt, id, field, value)
				continue
			}
			texts = append(texts, textCandidate{id: id, field: field, value: value, positive: reach["positive"] || reach["prompt"] || reach["text"]})
			continue
		}
		if isPrimitiveNumber(lowerClass) && titleMeansDuration(title) {
			if _, ok := inputs["value"]; ok {
				add(RoleDuration, id, "value", inputs["value"])
				continue
			}
		}
		for _, field := range sortedKeys(inputs) {
			value := inputs[field]
			if isLink(value) {
				continue
			}
			switch strings.ToLower(field) {
			case "duration", "seconds":
				add(RoleDuration, id, field, value)
			case "aspect_ratio", "ratio":
				add(RoleAspectRatio, id, field, value)
			case "seed", "noise_seed":
				add(RoleSeed, id, field, value)
			case "width":
				if isNumber(value) {
					add(RoleWidth, id, field, value)
				}
			case "height":
				if isNumber(value) {
					add(RoleHeight, id, field, value)
				}
			}
		}
	}
	// The main prompt is the text node that feeds a generation node's
	// positive/prompt input; ties go to the longest current text.
	sort.SliceStable(texts, func(i, j int) bool {
		if texts[i].positive != texts[j].positive {
			return texts[i].positive
		}
		return len(texts[i].value) > len(texts[j].value)
	})
	for i, text := range texts {
		role := RoleText
		if i == 0 {
			role = RolePrompt
		}
		add(role, text.id, text.field, text.value)
	}
	numberMediaBySlot(candidates, consumers)
	sort.SliceStable(candidates, func(i, j int) bool {
		if roleOrder(candidates[i].Role) != roleOrder(candidates[j].Role) {
			return roleOrder(candidates[i].Role) < roleOrder(candidates[j].Role)
		}
		return candidates[i].Index < candidates[j].Index
	})
	return candidates
}

var mediaSlotPattern = regexp.MustCompile(`(?:image|video|audio|ref)[a-z_.]*?(\d+)$`)

// numberMediaBySlot numbers loaders of one kind by the generation input slot
// they reach (ref_image_0, ref_image_1, image_2...) instead of by node ID, so
// @image1 is the loader wired to the first slot. Loaders without a numbered
// slot keep their node order after the numbered ones.
func numberMediaBySlot(candidates []InputCandidate, consumers map[string][][2]string) {
	for _, role := range []string{RoleImage, RoleVideo, RoleAudio} {
		type entry struct {
			pos, slot int
		}
		entries := make([]entry, 0)
		for pos, candidate := range candidates {
			if candidate.Role != role {
				continue
			}
			slot := -1
			for name := range downstreamInputNames(consumers, candidate.NodeID) {
				if match := mediaSlotPattern.FindStringSubmatch(name); match != nil {
					if value, err := strconv.Atoi(match[1]); err == nil && (slot < 0 || value < slot) {
						slot = value
					}
				}
			}
			entries = append(entries, entry{pos: pos, slot: slot})
		}
		sort.SliceStable(entries, func(i, j int) bool {
			a, b := entries[i], entries[j]
			if (a.slot >= 0) != (b.slot >= 0) {
				return a.slot >= 0
			}
			if a.slot >= 0 && a.slot != b.slot {
				return a.slot < b.slot
			}
			return candidates[a.pos].Index < candidates[b.pos].Index
		})
		for index, item := range entries {
			candidates[item.pos].Index = index
		}
	}
}

func disabledMediaInputs(editorNodes []editorNode, workflow map[string]map[string]any) []InputCandidate {
	candidates := make([]InputCandidate, 0)
	sorted := append([]editorNode(nil), editorNodes...)
	sort.SliceStable(sorted, func(i, j int) bool { return nodeIDLess(sorted[i].ID, sorted[j].ID) })
	for _, node := range sorted {
		if _, active := workflow[node.ID]; active {
			continue
		}
		if node.Mode != nodeModeBypassed && node.Mode != nodeModeMuted {
			continue
		}
		role := loaderRole(strings.ToLower(node.Type))
		if role == "" {
			continue
		}
		candidates = append(candidates, InputCandidate{
			Role: role, NodeID: node.ID, ClassType: node.Type, Title: node.Title,
			Field: defaultLoaderField(role, node.Type), Active: false,
		})
	}
	return candidates
}

func findOutputs(workflow map[string]map[string]any, ids []string, editorNodes []editorNode) []OutputCandidate {
	outputs := make([]OutputCandidate, 0)
	for _, id := range ids {
		classType := scalarString(workflow[id]["class_type"])
		if kind := outputKind(classType); kind != "" {
			outputs = append(outputs, OutputCandidate{NodeID: id, ClassType: classType, Title: apiNodeTitle(workflow[id]), Kind: kind, Active: true})
		}
	}
	for _, node := range editorNodes {
		if _, active := workflow[node.ID]; active || (node.Mode != nodeModeBypassed && node.Mode != nodeModeMuted) {
			continue
		}
		if kind := outputKind(node.Type); kind != "" {
			outputs = append(outputs, OutputCandidate{NodeID: node.ID, ClassType: node.Type, Title: node.Title, Kind: kind})
		}
	}
	return outputs
}

func mediaKindFromOutputs(outputs []OutputCandidate) string {
	found := map[string]bool{}
	for _, output := range outputs {
		if output.Active {
			found[output.Kind] = true
		}
	}
	for _, kind := range []string{KindVideo, KindImage, KindAudio} {
		if found[kind] {
			return kind
		}
	}
	return ""
}

func outputKind(classType string) string {
	lower := strings.ToLower(classType)
	switch {
	case lower == "vhs_videocombine", strings.Contains(lower, "savevideo"), strings.Contains(lower, "savewebm"),
		strings.Contains(lower, "saveanimated"), strings.Contains(lower, "videocombine"):
		return KindVideo
	case strings.Contains(lower, "saveaudio"), lower == "previewaudio":
		return KindAudio
	case strings.Contains(lower, "saveimage"), lower == "previewimage", lower == "image save":
		return KindImage
	}
	return ""
}

func loaderRole(lowerClass string) string {
	switch {
	case strings.Contains(lowerClass, "loadimage") && !strings.Contains(lowerClass, "mask"):
		return RoleImage
	case strings.Contains(lowerClass, "loadvideo"):
		return RoleVideo
	case strings.Contains(lowerClass, "loadaudio"):
		return RoleAudio
	}
	return ""
}

func loaderField(role string, inputs map[string]any) string {
	for _, field := range []string{role, "file", role + "_file", "path", "url"} {
		if value, ok := inputs[field]; ok && !isLink(value) {
			return field
		}
	}
	return ""
}

func defaultLoaderField(role, classType string) string {
	if role == RoleVideo && strings.EqualFold(classType, "LoadVideo") {
		return "file"
	}
	return role
}

func textField(lowerClass string, inputs map[string]any) (string, string, bool) {
	textual := strings.Contains(lowerClass, "text") || strings.Contains(lowerClass, "string") || strings.Contains(lowerClass, "prompt")
	if !textual {
		return "", "", false
	}
	for _, field := range []string{"text", "prompt", "string", "value", "text_positive"} {
		if value, ok := inputs[field].(string); ok {
			return field, value, true
		}
	}
	return "", "", false
}

func isPrimitiveNumber(lowerClass string) bool {
	switch lowerClass {
	case "primitivefloat", "primitiveint", "easy float", "easy int", "intconstant", "floatconstant", "primitivenode":
		return true
	}
	return false
}

func titleMeansDuration(lowerTitle string) bool {
	return strings.Contains(lowerTitle, "duration") || strings.Contains(lowerTitle, "second") || strings.Contains(lowerTitle, "(sec)")
}

// buildConsumers maps a node ID to the (consumer node, input name) pairs
// that read one of its outputs.
func buildConsumers(workflow map[string]map[string]any) map[string][][2]string {
	consumers := map[string][][2]string{}
	for id, node := range workflow {
		inputs, _ := node["inputs"].(map[string]any)
		for field, value := range inputs {
			if link, ok := value.([]any); ok && len(link) == 2 {
				source := scalarString(link[0])
				consumers[source] = append(consumers[source], [2]string{id, field})
			}
		}
	}
	return consumers
}

// downstreamInputNames walks the graph forward from a node (a few hops, enough
// to cross encoders and Get/Set helpers) and records the input names its value
// arrives through.
func downstreamInputNames(consumers map[string][][2]string, start string) map[string]bool {
	names := map[string]bool{}
	visited := map[string]bool{start: true}
	frontier := []string{start}
	for depth := 0; depth < 4 && len(frontier) > 0; depth++ {
		next := make([]string, 0)
		for _, id := range frontier {
			for _, edge := range consumers[id] {
				names[strings.ToLower(edge[1])] = true
				if !visited[edge[0]] {
					visited[edge[0]] = true
					next = append(next, edge[0])
				}
			}
		}
		frontier = next
	}
	return names
}

func roleOrder(role string) int {
	order := []string{RolePrompt, RoleNegativePrompt, RoleImage, RoleLastFrame, RoleVideo, RoleAudio, RoleDuration, RoleAspectRatio, RoleWidth, RoleHeight, RoleSeed, RoleText}
	for i, item := range order {
		if item == role {
			return i
		}
	}
	return len(order)
}

func hasRole(inputs []InputCandidate, role string, active bool) bool {
	for _, input := range inputs {
		if input.Role == role && input.Active == active {
			return true
		}
	}
	return false
}

func hasPackage(packages []PackageInfo, name string) bool {
	for _, pkg := range packages {
		if pkg.Name == name {
			return true
		}
	}
	return false
}

func addPackage(packages map[string]*PackageInfo, name, classType string, active bool) {
	if name == "" || name == "comfy-core" {
		return
	}
	pkg, ok := packages[name]
	if !ok {
		pkg = &PackageInfo{Name: name}
		packages[name] = pkg
	}
	pkg.Active = pkg.Active || active
	for _, existing := range pkg.NodeTypes {
		if existing == classType {
			return
		}
	}
	pkg.NodeTypes = append(pkg.NodeTypes, classType)
	sort.Strings(pkg.NodeTypes)
}

func editorPackage(node editorNode) string {
	if node.Properties == nil {
		return ""
	}
	for _, key := range []string{"cnr_id", "aux_id"} {
		if value := scalarString(node.Properties[key]); value != "" {
			return value
		}
	}
	if scalarString(node.Properties["ver"]) != "" {
		return "unknown (" + node.Type + ")"
	}
	return ""
}

var modelFileExtensions = []string{".safetensors", ".gguf", ".ckpt", ".pt", ".pth", ".bin", ".sft", ".onnx"}

func isModelFileName(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	for _, extension := range modelFileExtensions {
		if strings.HasSuffix(lower, extension) {
			return true
		}
	}
	return false
}

func widgetModelFiles(widgets any) []string {
	names := make([]string, 0)
	switch typed := widgets.(type) {
	case []any:
		for _, value := range typed {
			if name, ok := value.(string); ok && isModelFileName(name) {
				names = append(names, name)
			}
		}
	case map[string]any:
		for _, key := range sortedKeys(typed) {
			if name, ok := typed[key].(string); ok && isModelFileName(name) {
				names = append(names, name)
			}
		}
	}
	return names
}

func apiNodeTitle(node map[string]any) string {
	meta, _ := node["_meta"].(map[string]any)
	return scalarString(meta["title"])
}

func scalarString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case nil:
		return ""
	default:
		return ""
	}
}

func isLink(value any) bool {
	link, ok := value.([]any)
	return ok && len(link) == 2
}

func isNumber(value any) bool {
	switch value.(type) {
	case float64, int, int64:
		return true
	}
	return false
}

func shortValue(value any) any {
	if text, ok := value.(string); ok && len(text) > 200 {
		return text[:200] + "..."
	}
	return value
}

func numberValueType(classType string, current any) string {
	lower := strings.ToLower(classType)
	if strings.Contains(lower, "float") {
		return "float"
	}
	if strings.Contains(lower, "int") {
		return "int"
	}
	if number, ok := current.(float64); ok && number != float64(int64(number)) {
		return "float"
	}
	return "int"
}

// defaultAspectRatioEnum pre-fills the ratio map for ResolutionSelector-style
// nodes whose options read like "16:9 (Widescreen)". The labels come from the
// node's current value pattern; the admin should check them against the node.
func defaultAspectRatioEnum(classType string, current any) map[string]string {
	value, _ := current.(string)
	if !strings.Contains(value, "(") {
		return nil
	}
	return map[string]string{
		"1:1":  "1:1 (Square)",
		"3:2":  "3:2 (Photo)",
		"4:3":  "4:3 (Standard)",
		"16:9": "16:9 (Widescreen)",
		"21:9": "21:9 (Ultrawide)",
		"2:3":  "2:3 (Portrait Photo)",
		"3:4":  "3:4 (Portrait Standard)",
		"9:16": "9:16 (Portrait Widescreen)",
	}
}

func sortedNodeIDs(workflow map[string]map[string]any) []string {
	ids := make([]string, 0, len(workflow))
	for id := range workflow {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return nodeIDLess(ids[i], ids[j]) })
	return ids
}

func nodeIDLess(a, b string) bool {
	left, leftErr := strconv.Atoi(a)
	right, rightErr := strconv.Atoi(b)
	if leftErr == nil && rightErr == nil {
		return left < right
	}
	return a < b
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
