package batoconf

import (
	"regexp"
	"strconv"
	"strings"
)

// Control types: how the web app edits a key.
const (
	ControlToggle   = "toggle"   // "1" or "0"
	ControlSelect   = "select"   // one of Choices
	ControlCombo    = "combo"    // Choices are suggestions; any value is accepted
	ControlRange    = "range"    // a number between Min and Max
	ControlNumber   = "number"   // a whole number
	ControlText     = "text"     // free text
	ControlPassword = "password" // free text, hidden
	ControlScreen   = "screen"   // edited on another BatoX screen (Screen)
)

// Choice is one value of a select or combo.
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

// Control tells the web app how to edit a key.
type Control struct {
	Type    string   `json:"type"`
	Label   string   `json:"label,omitempty"`   // a readable name, from es_features.cfg
	Choices []Choice `json:"choices,omitempty"` // select and combo
	Min     *float64 `json:"min,omitempty"`     // range
	Max     *float64 `json:"max,omitempty"`     // range
	Step    float64  `json:"step,omitempty"`    // range
	Unit    string   `json:"unit,omitempty"`    // range, such as "%"
	Screen  string   `json:"screen,omitempty"`  // "audio" or "video"
}

// Keys that BatoX edits on screens of their own, with the right checks.
var screenKeys = map[string]string{
	"audio.device":        "audio",
	"global.videooutput":  "video",
	"global.videooutput2": "video",
	"global.videooutput3": "video",
}

var (
	// "## suspend -> Enter low-power standby mode." The value has no spaces,
	// which leaves out lines such as "Leave commented out -> Auto."
	choiceRe = regexp.MustCompile(`^(\S+)\s+->\s*(.*)$`)
	// "Set system volume (0-100)"
	rangeRe = regexp.MustCompile(`\((-?\d+)\s*-\s*(-?\d+)\)`)
	// "Activate Wi-Fi (0,1)"
	boolRe = regexp.MustCompile(`\(\s*0\s*,\s*1\s*\)`)
	// Words that make a 0 or 1 a quantity rather than off or on.
	quantityRe = regexp.MustCompile(`(?i)\b(seconds?|minutes?|ms|milliseconds?|delay|timeout|time|dpi|brightness|volume|size|count|watts?|tdp)\b`)
	// Choices given as examples: other values are accepted too.
	examplesRe = regexp.MustCompile(`(?i)\b(examples?|e\.g\.|for more)\b`)
	integerRe  = regexp.MustCompile(`^-?\d+$`)
)

// inferControl picks a control from the key, its value and the comment lines
// above it, and returns the lines left to describe it (choices are shown by
// the control itself).
func inferControl(key, value string, sensitive bool, lines []string) (Control, []string) {
	if screen, ok := screenKeys[key]; ok {
		return Control{Type: ControlScreen, Screen: screen}, lines
	}
	if sensitive {
		return Control{Type: ControlPassword}, lines
	}

	var choices []Choice
	var rest []string
	for _, l := range lines {
		if m := choiceRe.FindStringSubmatch(l); m != nil {
			choices = append(choices, Choice{Value: m[1], Label: strings.TrimSpace(m[2])})
			continue
		}
		rest = append(rest, l)
	}
	desc := strings.Join(rest, " ")
	examples := examplesRe.MatchString(desc)
	if len(choices) >= 2 {
		switch {
		case isOnOff(choices):
			return Control{Type: ControlToggle}, rest
		case examples:
			return Control{Type: ControlCombo, Choices: choices}, rest
		case value == "" || hasChoice(choices, value):
			return Control{Type: ControlSelect, Choices: choices}, rest
		}
		// The value isn't one of them: they describe parts of it (such as
		// "s,b,f" for "s -> snapshot"), so they stay in the description.
	}

	all := strings.Join(lines, " ")
	if m := rangeRe.FindStringSubmatch(all); m != nil && (value == "" || integerRe.MatchString(value)) {
		lo, _ := strconv.ParseFloat(m[1], 64)
		hi, _ := strconv.ParseFloat(m[2], 64)
		if lo < hi {
			return Control{Type: ControlRange, Min: &lo, Max: &hi, Step: 1}, lines
		}
	}
	if value == "0" || value == "1" {
		if boolRe.MatchString(all) || !quantityRe.MatchString(all+" "+strings.ReplaceAll(key, ".", " ")) {
			return Control{Type: ControlToggle}, lines
		}
	}
	if integerRe.MatchString(value) {
		return Control{Type: ControlNumber}, lines
	}
	return Control{Type: ControlText}, lines
}

func isOnOff(choices []Choice) bool {
	if len(choices) != 2 {
		return false
	}
	a, b := choices[0].Value, choices[1].Value
	return (a == "0" && b == "1") || (a == "1" && b == "0")
}

func hasChoice(choices []Choice, v string) bool {
	for _, c := range choices {
		if c.Value == v {
			return true
		}
	}
	return false
}
