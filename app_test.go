package gekko

import (
	"fmt"
	"reflect"
	"testing"
	"time"

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

func TestComputeFramePacingSleep(t *testing.T) {
	targetFrameTime := time.Second / 60

	assert.Equal(t, time.Duration(0), computeFramePacingSleep(5*time.Millisecond, 0))
	assert.Equal(t, time.Duration(0), computeFramePacingSleep(targetFrameTime, targetFrameTime))
	assert.Equal(t, time.Duration(0), computeFramePacingSleep(20*time.Millisecond, targetFrameTime))
	assert.Equal(t, targetFrameTime-5*time.Millisecond, computeFramePacingSleep(5*time.Millisecond, targetFrameTime))
}

func TestAppProfileCategorySummary(t *testing.T) {
	got := appProfileCategorySummary([]appSystemTiming{
		{category: "ai", duration: time.Millisecond},
		{category: "motor_collision", duration: 2 * time.Millisecond},
		{category: "ai", duration: 500 * time.Microsecond},
		{duration: 250 * time.Microsecond},
	})
	assert.Equal(t, "ai=1.50ms,motor_collision=2.00ms,other=0.25ms", got)
}

func TestAppPublishesCompletedFrameProfile(t *testing.T) {
	profile := &FrameProfile{Categories: map[string]time.Duration{"stale": time.Second}}
	app := NewApp()
	app.slowFrameThreshold = 10 * time.Millisecond
	app.addResources(profile)
	app.profileSystems = []appSystemTiming{
		{category: "ai", duration: 2 * time.Millisecond},
		{category: "ai", duration: time.Millisecond},
	}
	app.reportSlowFrame(4*time.Millisecond, 5*time.Millisecond)
	assert.Equal(t, 4*time.Millisecond, profile.Work)
	assert.Equal(t, 5*time.Millisecond, profile.RawDelta)
	assert.Equal(t, map[string]time.Duration{"ai": 3 * time.Millisecond}, profile.Categories)
}
