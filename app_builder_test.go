package gekko

import "testing"

type MockModule struct {
	installed bool
}

func (m *MockModule) Install(app *App, commands *Commands) {
	m.installed = true
}

type MockModule2 struct {
	installed bool
}

func (m *MockModule2) Install(app *App, commands *Commands) {
	m.installed = true
}
func TestAppBuilder_Stateless(t *testing.T) {
	builder := NewAppBuilder()
	app := builder.Build()

	if app.stateful != false {
		t.Errorf("Expected stateful to be false, got %v", app.stateful)
	}
	if app.initialState != 0 {
		t.Errorf("Expected initialState to be 0, got %v", app.initialState)
	}
	if app.finalState != 0 {
		t.Errorf("Expected finalState to be 0, got %v", app.finalState)
	}
}

func TestAppBuilder_UseStates(t *testing.T) {
	builder := NewAppBuilder()
	builder.UseStates(1, 10)

	app := builder.Build()

	if app.stateful != true {
		t.Errorf("Expected stateful to be true, got %v", app.stateful)
	}
	if app.initialState != 1 {
		t.Errorf("Expected initialState to be 1, got %v", app.initialState)
	}
	if app.finalState != 10 {
		t.Errorf("Expected finalState to be 10, got %v", app.finalState)
	}
}

func TestAppBuilder_UseModule(t *testing.T) {
	builder := NewAppBuilder()
	mockModule := &MockModule{}
	builder.UseModule(mockModule)

	if len(builder.modules) != 1 {
		t.Errorf("Expected modules to contain 1 module, got %v", len(builder.modules))
	}
}
func TestAppBuilder_Build_WithModules(t *testing.T) {
	builder := NewAppBuilder()
	module := &MockModule{}
	builder.UseModule(module)

	builder.Build()

	// Ensure Install() method of the module is called (you can use mocking frameworks like `testify` to track this)
	if len(builder.modules) != 1 {
		t.Errorf("Expected modules to contain 1 module, got %v", len(builder.modules))
	}
	if !module.installed {
		t.Errorf("Expected Install to be called on the module, but it was not")
	}
}

func TestAppBuilder_Build_WithMultipleModules(t *testing.T) {
	module1 := &MockModule{}
	module2 := &MockModule{}

	builder := NewAppBuilder()
	builder.UseModule(module1)
	builder.UseModule(module2)

	builder.Build()

	if len(builder.modules) != 2 {
		t.Errorf("Expected 2 modules, got %v", len(builder.modules))
	}
	if !module1.installed {
		t.Errorf("Expected Install to be called on the module 1, but it was not")
	}
	if !module2.installed {
		t.Errorf("Expected Install to be called on the module 2, but it was not")
	}
}
