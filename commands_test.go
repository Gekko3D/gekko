package gekko

import (
	"reflect"
	"testing"
)

func TestCommandsAddComponentsIgnoresTypedNilComponents(t *testing.T) {
	type TestComponent struct {
		Value string
	}

	app := NewApp()
	cmd := app.Commands()
	eid := cmd.AddEntity(&TransformComponent{})
	app.FlushCommands()

	var nilComponent *TestComponent
	cmd.AddComponents(eid, nilComponent)
	app.FlushCommands()

	if got := cmd.GetComponent(eid, reflect.TypeOf(TestComponent{})); got != nil {
		t.Fatalf("expected typed-nil component to be ignored, got %#v", got)
	}
}

func TestCommandsAddEntityIgnoresTypedNilComponents(t *testing.T) {
	type TestComponent struct {
		Value string
	}

	app := NewApp()
	cmd := app.Commands()

	var nilComponent *TestComponent
	eid := cmd.AddEntity(nilComponent)
	app.FlushCommands()

	if got := cmd.GetComponent(eid, reflect.TypeOf(TestComponent{})); got != nil {
		t.Fatalf("expected typed-nil component to be ignored on entity add, got %#v", got)
	}
}

func TestCommandsAddComponentsToRemovedEntityDoesNotPanicOnFlush(t *testing.T) {
	type TestComponent struct {
		Value string
	}

	app := NewApp()
	cmd := app.Commands()
	eid := cmd.AddEntity(&TransformComponent{})
	app.FlushCommands()

	cmd.RemoveEntity(eid)
	cmd.AddComponents(eid, &TestComponent{Value: "stale"})
	app.FlushCommands()

	if got := cmd.GetComponent(eid, reflect.TypeOf(TestComponent{})); got != nil {
		t.Fatalf("expected removed entity to stay removed, got %#v", got)
	}
}
