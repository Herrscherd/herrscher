package vendortest

import (
	"errors"
	"strconv"
	"strings"
)

var (
	errUnsupportedStep  = errors.New("this fake replays only text, echo and error steps")
	errSpawnUnsupported = errors.New("spawn steps are served by the claude fake only")
)

type vendor struct {
	label  string
	files  Files
	script Script
}

func openVendor(label string, files Files) (*vendor, error) {
	script, err := loadScript(files.Script)
	if err != nil {
		return nil, err
	}
	return &vendor{label: label, files: files, script: script}, nil
}

func (v *vendor) record(line string) error {
	if v.files.Log == "" {
		return nil
	}
	return appendLine(v.files.Log, v.label+" "+line)
}

func (v *vendor) recordPrompt(prompt string) error {
	return v.record("prompt " + strconv.Quote(prompt))
}

func (v *vendor) nextTurn() (Turn, error) {
	return nextTurnOf(v.script, v.files)
}

type vendorReply struct {
	text    string
	failure string
	failed  bool
}

func (v *vendor) scripted(turn Turn, prompt string) (vendorReply, error) {
	if len(turn.spawns) > 0 {
		return vendorReply{}, errSpawnUnsupported
	}
	var texts []string
	for _, current := range turn.steps {
		switch current.kind {
		case stepText:
			texts = append(texts, current.text)
		case stepEcho:
			texts = append(texts, echoPrefix+lastLine(prompt))
		case stepFail:
			return vendorReply{failure: current.text, failed: true}, nil
		case stepTool, stepHang, stepCrash, stepAPIError:
			return vendorReply{}, errUnsupportedStep
		}
	}
	return vendorReply{text: strings.Join(texts, "\n\n")}, nil
}
