package gekko

import "testing"

func TestInputPasteShortcutSupportsControlAndSuper(t *testing.T) {
	ctrl := &Input{}
	ctrl.Pressed[KeyControl] = true
	ctrl.JustPressed[KeyV] = true
	if !inputPasteShortcutPressed(ctrl) {
		t.Fatal("expected Ctrl+V to trigger paste")
	}

	super := &Input{}
	super.Pressed[KeySuper] = true
	super.JustPressed[KeyV] = true
	if !inputPasteShortcutPressed(super) {
		t.Fatal("expected Super+V to trigger paste")
	}

	plain := &Input{}
	plain.JustPressed[KeyV] = true
	if inputPasteShortcutPressed(plain) {
		t.Fatal("did not expect plain V to trigger paste")
	}
}
