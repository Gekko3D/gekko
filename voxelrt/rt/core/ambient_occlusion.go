package core

type AmbientOcclusionMode uint32

const (
	AmbientOcclusionModeDefault AmbientOcclusionMode = iota
	AmbientOcclusionModeEnabled
	AmbientOcclusionModeDisabled
)
