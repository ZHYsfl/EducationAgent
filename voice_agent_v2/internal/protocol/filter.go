package protocol

import "strings"

// ProtocolFilter is a streaming filter that strips #{...} and @{...} blocks
// from the token stream for display/TTS purposes.
type ProtocolFilter struct {
	inThink  bool
	inAction bool
	partial  string
}

// Feed processes one streaming token and returns the visible portion.
func (f *ProtocolFilter) Feed(token string) string {
	text := f.partial + token
	f.partial = ""

	var visible strings.Builder

	for len(text) > 0 {
		if f.inThink {
			idx := strings.Index(text, "}")
			if idx >= 0 {
				f.inThink = false
				text = text[idx+1:]
			} else {
				text = ""
			}
		} else if f.inAction {
			idx := strings.Index(text, "}")
			if idx >= 0 {
				f.inAction = false
				text = text[idx+1:]
			} else {
				text = ""
			}
		} else {
			thinkIdx := strings.Index(text, "#{")
			actionIdx := strings.Index(text, "@{")

			minIdx := -1
			if thinkIdx >= 0 && (actionIdx < 0 || thinkIdx < actionIdx) {
				minIdx = thinkIdx
				f.inThink = true
			} else if actionIdx >= 0 {
				minIdx = actionIdx
				f.inAction = true
			}

			if minIdx >= 0 {
				visible.WriteString(text[:minIdx])
				text = text[minIdx+2:]
			} else {
				// Check for partial marker at end
				if strings.HasSuffix(text, "#") || strings.HasSuffix(text, "@") {
					visible.WriteString(text[:len(text)-1])
					f.partial = text[len(text)-1:]
					text = ""
				} else {
					visible.WriteString(text)
					text = ""
				}
			}
		}
	}
	return visible.String()
}
