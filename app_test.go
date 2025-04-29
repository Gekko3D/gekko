package gekko

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockResource1 struct {
	name string
}
type MockResource2 struct {
	name string
}

func NewMockResource1(name string) *MockResource1 {
	return &MockResource1{name: name}
}
func NewMockResource2(name string) *MockResource2 {
	return &MockResource2{name: name}
}

func TestApp_changeState(t *testing.T) {
	app := &App{
		stateful:     true,
		initialState: 1,
		state:        1,
		finalState:   2,
	}

	// Test changing state
	app.changeState(2)
	if app.nextState != State(2) {
		t.Errorf("The nextState should be set correctly.")
	}
	if !app.stateTransitioning {
		t.Errorf("The stateTransitioning flag should be true.")
	}

	// Test executing state change
	app.executeChangeState(2)
	if app.state != State(2) {
		t.Errorf("The app state should change correctly.")
	}
}

func TestApp_addResources(t *testing.T) {
	// Test setup
	app := &App{
		resources: make(map[reflect.Type]any),
	}

	// Add a resource
	resource1 := NewMockResource1("Resource1")
	app.addResources(resource1)

	// Check that the resource was added
	assert.Contains(t, app.resources, reflect.TypeOf(resource1).Elem(), "Resource1 should be in resources map.")

	// Expect panic when trying to add the same type of resource again
	require.PanicsWithValue(t, fmt.Sprintf("%s is already in resources", reflect.TypeOf(resource1)), func() {
		app.addResources(resource1) // Try adding resource1 again, should panic
	})

	// Add a resource
	resource2 := NewMockResource2("Resource2")
	app.addResources(resource2)

	// Check that the resource was added
	assert.Contains(t, app.resources, reflect.TypeOf(resource2).Elem(), "Resource2 should be in resources map.")
}
