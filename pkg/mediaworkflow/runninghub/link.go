package runninghub

import (
	"regexp"
	"strings"
)

// LinkInfo is what a pasted RunningHub link (or bare ID) points to.
type LinkInfo struct {
	ID string `json:"id"`
	// Kind is "workflow" (usable as workflowId), "post" (a community post:
	// open it, clone the workflow into your account, then use the clone's
	// /workflow/<id> link) or "app" (an AI app / webappId).
	Kind string `json:"kind"`
}

var (
	linkPatterns = []struct {
		kind    string
		pattern *regexp.Regexp
	}{
		{"workflow", regexp.MustCompile(`/workflow/(\d{6,})`)},
		{"post", regexp.MustCompile(`/post/(\d{6,})`)},
		{"app", regexp.MustCompile(`/(?:ai-detail|app|webapp)/(\d{6,})`)},
	}
	bareID = regexp.MustCompile(`^\d{6,}$`)
)

// ParseLink extracts the ID from links such as
// https://www.runninghub.ai/workflow/2108242199912755201,
// https://www.runninghub.ai/#/workflow/<id>?..., /post/<id> or a bare ID.
func ParseLink(link string) (LinkInfo, bool) {
	link = strings.TrimSpace(link)
	if bareID.MatchString(link) {
		return LinkInfo{ID: link, Kind: "workflow"}, true
	}
	for _, candidate := range linkPatterns {
		if match := candidate.pattern.FindStringSubmatch(link); match != nil {
			return LinkInfo{ID: match[1], Kind: candidate.kind}, true
		}
	}
	return LinkInfo{}, false
}
