package mediaworkflow

import (
	"regexp"
	"strings"
)

// PromptTemplate adapts the prompt KSB sends to what a workflow expects
// without changing KSB. Empty fields mean passthrough.
type PromptTemplate struct {
	// Template wraps the prompt; "{{prompt}}" is replaced by the (rewritten)
	// request prompt. Empty means "{{prompt}}".
	Template string `json:"template,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	Suffix   string `json:"suffix,omitempty"`
	// ReferenceTags rewrites KSB reference tags such as @image1 / @video2 /
	// @audio1. Keys are the media kind (image, video, audio); values use {n}
	// for the 1-based number, e.g. {"image": "<Picture {n}>"}. Kinds that are
	// not listed are left as written.
	ReferenceTags map[string]string `json:"reference_tags,omitempty"`
}

var referenceTagPattern = regexp.MustCompile(`@(image|video|audio)(\d+)`)

// Render applies tag rewriting, the template, then prefix and suffix. An
// empty prompt stays empty so workflows keep their own default text.
func (t PromptTemplate) Render(prompt string) string {
	if strings.TrimSpace(prompt) == "" {
		return prompt
	}
	if len(t.ReferenceTags) > 0 {
		prompt = referenceTagPattern.ReplaceAllStringFunc(prompt, func(tag string) string {
			match := referenceTagPattern.FindStringSubmatch(tag)
			format, ok := t.ReferenceTags[match[1]]
			if !ok || strings.TrimSpace(format) == "" {
				return tag
			}
			return strings.ReplaceAll(format, "{n}", match[2])
		})
	}
	if strings.TrimSpace(t.Template) != "" {
		prompt = strings.ReplaceAll(t.Template, "{{prompt}}", prompt)
	}
	return t.Prefix + prompt + t.Suffix
}
