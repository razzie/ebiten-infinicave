package render

import (
	_ "embed"
)

//go:embed shaders/grain.kage
var MaterialShaderSource []byte

//go:embed shaders/vines.kage
var VineShaderSource []byte

//go:embed shaders/blur.kage
var blurShaderSource []byte

//go:embed shaders/background_fade.kage
var BackgroundFadeShaderSource []byte

//go:embed shaders/background.kage
var backgroundShaderSource []byte

//go:embed shaders/fog.kage
var fogShaderSource []byte
