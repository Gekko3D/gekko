package gekko

import (
	"github.com/go-gl/glfw/v3.3/glfw"
)

const (
	KeyA int = iota
	KeyB
	KeyC
	KeyD
	KeyE
	KeyF
	KeyG
	KeyH
	KeyI
	KeyJ
	KeyK
	KeyL
	KeyM
	KeyN
	KeyO
	KeyP
	KeyQ
	KeyR
	KeyS
	KeyT
	KeyU
	KeyV
	KeyW
	KeyX
	KeyY
	KeyZ
	Key0
	Key1
	Key2
	Key3
	Key4
	Key5
	Key6
	Key7
	Key8
	Key9
	KeySpace
	KeyEnter
	KeyEscape
	KeyTab
	KeyBackspace
	KeyInsert
	KeyDelete
	KeyRight
	KeyLeft
	KeyDown
	KeyUp
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
	KeyMinus
	KeyEqual
	KeyKPPlus
	KeyKPMinus
	KeyShift
	KeyControl
	KeyLeftAlt
	MouseButtonLeft
	MouseButtonRight
	MouseButtonMiddle
)

type InputModule struct{}

type Input struct {
	Pressed [256]bool

	JustPressed  [256]bool
	JustReleased [256]bool

	MouseX, MouseY           float64
	MouseDeltaX, MouseDeltaY float64
	MouseCaptured            bool

	WindowWidth, WindowHeight int
	CharBuffer                []rune
}

func (mod InputModule) Install(app *App, cmd *Commands) {
	cmd.AddResources(&Input{})
	app.UseSystem(
		System(inputSystem).
			InStage(PreUpdate).
			RunAlways(),
	)
}

func inputSystem(s *WindowState, input *Input) {
	input.CharBuffer = nil // Clear buffer at start of frame

	// Register character callback if not already set
	s.windowGlfw.SetCharCallback(func(w *glfw.Window, char rune) {
		input.CharBuffer = append(input.CharBuffer, char)
	})

	glfw.PollEvents()

	// Update Keyboard
	for key, glfwKey := range keyToGlfw {
		action := s.windowGlfw.GetKey(glfwKey)

		input.JustPressed[key] = false
		input.JustReleased[key] = false

		if glfw.Press == action {
			if !input.Pressed[key] {
				input.JustPressed[key] = true
			}
			input.Pressed[key] = true
		} else if glfw.Release == action {
			if input.Pressed[key] {
				input.JustReleased[key] = true
			}
			input.Pressed[key] = false
		}
	}

	// Update Mouse
	mx, my := s.windowGlfw.GetCursorPos()
	if input.MouseCaptured {
		input.MouseDeltaX = mx - input.MouseX
		input.MouseDeltaY = my - input.MouseY
	} else {
		input.MouseDeltaX = 0
		input.MouseDeltaY = 0
	}
	input.MouseX = mx
	input.MouseY = my

	// Update window dimensions
	input.WindowWidth, input.WindowHeight = s.windowGlfw.GetSize()

	// Update mouse buttons
	for btn := MouseButtonLeft; btn <= MouseButtonMiddle; btn++ {
		var glfwBtn glfw.MouseButton
		switch btn {
		case MouseButtonLeft:
			glfwBtn = glfw.MouseButtonLeft
		case MouseButtonRight:
			glfwBtn = glfw.MouseButtonRight
		case MouseButtonMiddle:
			glfwBtn = glfw.MouseButtonMiddle
		}

		action := s.windowGlfw.GetMouseButton(glfwBtn)
		input.JustPressed[btn] = false
		input.JustReleased[btn] = false

		if glfw.Press == action {
			if !input.Pressed[btn] {
				input.JustPressed[btn] = true
			}
			input.Pressed[btn] = true
		} else if glfw.Release == action {
			if input.Pressed[btn] {
				input.JustReleased[btn] = true
			}
			input.Pressed[btn] = false
		}
	}

	if input.MouseCaptured {
		s.windowGlfw.SetInputMode(glfw.CursorMode, glfw.CursorDisabled)
	} else {
		s.windowGlfw.SetInputMode(glfw.CursorMode, glfw.CursorNormal)
	}
}

var keyToGlfw = map[int]glfw.Key{
	KeyA:         glfw.KeyA,
	KeyB:         glfw.KeyB,
	KeyC:         glfw.KeyC,
	KeyD:         glfw.KeyD,
	KeyE:         glfw.KeyE,
	KeyF:         glfw.KeyF,
	KeyG:         glfw.KeyG,
	KeyH:         glfw.KeyH,
	KeyI:         glfw.KeyI,
	KeyJ:         glfw.KeyJ,
	KeyK:         glfw.KeyK,
	KeyL:         glfw.KeyL,
	KeyM:         glfw.KeyM,
	KeyN:         glfw.KeyN,
	KeyO:         glfw.KeyO,
	KeyP:         glfw.KeyP,
	KeyQ:         glfw.KeyQ,
	KeyR:         glfw.KeyR,
	KeyS:         glfw.KeyS,
	KeyT:         glfw.KeyT,
	KeyU:         glfw.KeyU,
	KeyV:         glfw.KeyV,
	KeyW:         glfw.KeyW,
	KeyX:         glfw.KeyX,
	KeyY:         glfw.KeyY,
	KeyZ:         glfw.KeyZ,
	Key0:         glfw.Key0,
	Key1:         glfw.Key1,
	Key2:         glfw.Key2,
	Key3:         glfw.Key3,
	Key4:         glfw.Key4,
	Key5:         glfw.Key5,
	Key6:         glfw.Key6,
	Key7:         glfw.Key7,
	Key8:         glfw.Key8,
	Key9:         glfw.Key9,
	KeySpace:     glfw.KeySpace,
	KeyEnter:     glfw.KeyEnter,
	KeyEscape:    glfw.KeyEscape,
	KeyTab:       glfw.KeyTab,
	KeyBackspace: glfw.KeyBackspace,
	KeyInsert:    glfw.KeyInsert,
	KeyDelete:    glfw.KeyDelete,
	KeyRight:     glfw.KeyRight,
	KeyLeft:      glfw.KeyLeft,
	KeyDown:      glfw.KeyDown,
	KeyUp:        glfw.KeyUp,
	KeyF1:        glfw.KeyF1,
	KeyF2:        glfw.KeyF2,
	KeyF3:        glfw.KeyF3,
	KeyF4:        glfw.KeyF4,
	KeyF5:        glfw.KeyF5,
	KeyF6:        glfw.KeyF6,
	KeyF7:        glfw.KeyF7,
	KeyF8:        glfw.KeyF8,
	KeyF9:        glfw.KeyF9,
	KeyF10:       glfw.KeyF10,
	KeyF11:       glfw.KeyF11,
	KeyF12:       glfw.KeyF12,
	KeyMinus:     glfw.KeyMinus,
	KeyEqual:     glfw.KeyEqual,
	KeyKPPlus:    glfw.KeyKPAdd,
	KeyKPMinus:   glfw.KeyKPSubtract,
	KeyShift:     glfw.KeyLeftShift,
	KeyControl:   glfw.KeyLeftControl,
	KeyLeftAlt:   glfw.KeyLeftAlt,
}
