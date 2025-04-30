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
func TestAppBuilding_Stateless(t *testing.T) {
	app := NewApp()
	app.build()

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

func TestAppBuilding_UseStates(t *testing.T) {
	app := NewApp().UseStates(1, 10)
	app.build()

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

func TestAppBuilding_UseModule(t *testing.T) {
	app := NewApp().UseModules(&MockModule{})

	if len(app.modules) != 1 {
		t.Errorf("Expected modules to contain 1 module, got %v", len(app.modules))
	}
}
func TestAppBuilding_Build_WithModules(t *testing.T) {
	module := &MockModule{}
	app := NewApp().UseModules(module)
	app.build()

	// Ensure Install() method of the module is called (you can use mocking frameworks like `testify` to track this)
	if len(app.modules) != 1 {
		t.Errorf("Expected modules to contain 1 module, got %v", len(app.modules))
	}
	if !module.installed {
		t.Errorf("Expected Install to be called on the module, but it was not")
	}
}

func TestAppBuilding_Build_WithMultipleModules(t *testing.T) {
	module1 := &MockModule{}
	module2 := &MockModule{}
	module3 := &MockModule{}

	app := NewApp().UseModules(module1, module2).UseModules(module3)
	app.build()

	if 3 != len(app.modules) {
		t.Errorf("Expected 3 modules, got %v", len(app.modules))
	}
	if !module1.installed {
		t.Errorf("Expected Install to be called on the module 1, but it was not")
	}
	if !module2.installed {
		t.Errorf("Expected Install to be called on the module 2, but it was not")
	}
	if !module3.installed {
		t.Errorf("Expected Install to be called on the module 3, but it was not")
	}
}
