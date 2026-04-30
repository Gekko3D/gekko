package core

import (
	"fmt"
	"strings"
)

type DepthMode string

const (
	DepthModeStandard DepthMode = "standard"
	DepthModeReverseZ DepthMode = "reverse-z"
)

func (m DepthMode) Normalized() DepthMode {
	switch strings.TrimSpace(strings.ToLower(string(m))) {
	case "", string(DepthModeStandard):
		return DepthModeStandard
	case string(DepthModeReverseZ):
		return DepthModeReverseZ
	default:
		return DepthModeStandard
	}
}

func (m DepthMode) String() string {
	return string(m.Normalized())
}

func ParseDepthMode(raw string) (DepthMode, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "":
		return DepthModeStandard, nil
	case string(DepthModeStandard):
		return DepthModeStandard, nil
	case string(DepthModeReverseZ):
		return DepthModeReverseZ, nil
	default:
		return DepthModeStandard, fmt.Errorf("invalid depth mode %q", raw)
	}
}
