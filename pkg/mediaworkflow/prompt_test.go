package mediaworkflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPromptTemplateRender(t *testing.T) {
	tests := []struct {
		name     string
		template PromptTemplate
		prompt   string
		want     string
	}{
		{name: "passthrough", template: PromptTemplate{}, prompt: "@image1 smiles", want: "@image1 smiles"},
		{
			name:     "rewrite reference tags",
			template: PromptTemplate{ReferenceTags: map[string]string{"image": "<Picture {n}>", "video": "<Video {n}>"}},
			prompt:   "@image1 dances like @video2, voice @audio1, @image12",
			want:     "<Picture 1> dances like <Video 2>, voice @audio1, <Picture 12>",
		},
		{
			name:     "template prefix suffix",
			template: PromptTemplate{Template: "summary: {{prompt}}", Prefix: "trigger\n\n", Suffix: "\nend"},
			prompt:   "a cat",
			want:     "trigger\n\nsummary: a cat\nend",
		},
		{name: "empty prompt stays empty", template: PromptTemplate{Prefix: "trigger "}, prompt: "  ", want: "  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.template.Render(tt.prompt))
		})
	}
}
