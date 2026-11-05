package presence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openhue/openhue-go"
	"lampengeist/internal/config"
	"lampengeist/internal/schedules"
	"lampengeist/internal/storage"
)

func TestIsTransientUpdateError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil",
			err:  nil,
			want: false,
		},
		{
			name: "multi status",
			err:  &openhue.ApiError{StatusCode: http.StatusMultiStatus, Status: "207 Multi-Status"},
			want: true,
		},
		{
			name: "wrapped multi status",
			err:  fmt.Errorf("update failed: %w", &openhue.ApiError{StatusCode: http.StatusMultiStatus}),
			want: true,
		},
		{
			name: "bad request",
			err:  &openhue.ApiError{StatusCode: http.StatusBadRequest},
			want: false,
		},
		{
			name: "generic error",
			err:  errors.New("boom"),
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isTransientUpdateError(test.err); got != test.want {
				t.Fatalf("isTransientUpdateError(%v) = %v, want %v", test.err, got, test.want)
			}
		})
	}
}

func TestScaledDelay(t *testing.T) {
	simulated := 1 * time.Minute

	if got, want := scaledDelay(simulated, 1), simulated; got != want {
		t.Fatalf("1x delay = %v, want %v", got, want)
	}
	if got, want := scaledDelay(simulated, 60), time.Second; got != want {
		t.Fatalf("60x delay = %v, want %v", got, want)
	}
	if got, want := scaledDelay(simulated, config.MaxSimulationSpeed), simulated/config.MaxSimulationSpeed; got != want {
		t.Fatalf("max speed delay = %v, want %v", got, want)
	}
	if got, want := scaledDelay(simulated, 0), simulated; got != want {
		t.Fatalf("invalid speed delay = %v, want %v", got, want)
	}
	if got := scaledDelay(-1*time.Minute, 60); got != 0 {
		t.Fatalf("overdue delay = %v, want 0", got)
	}
}

// TestScheduleNextSpacesStepsOut covers the floor that protects the bridge at
// high speed factors, and the fact that it is measured from the last step: a
// stream of speed changes recomputes the wait, and recomputing must not keep
// pushing the step away.
func TestScheduleNextSpacesStepsOut(t *testing.T) {
	start := time.Date(2024, 1, 15, 8, 0, 0, 0, time.Local)
	clock := start

	sim := newTestSimulator(config.MaxSimulationSpeed, &clock)
	lastStep := start
	due := start.Add(2 * time.Minute) // simulated: ~83ms of real time at 1440x

	if got := sim.scheduleNext(due, lastStep); got != minStepSpacing {
		t.Fatalf("first wait = %v, want the %v floor", got, minStepSpacing)
	}

	// Recomputing after most of the floor has elapsed must leave only the
	// remainder, not restart it.
	clock = start.Add(80 * time.Millisecond)
	if got, want := sim.scheduleNext(due, lastStep), 20*time.Millisecond; got != want {
		t.Fatalf("recomputed wait = %v, want %v", got, want)
	}

	clock = start.Add(150 * time.Millisecond)
	if got := sim.scheduleNext(due, lastStep); got != 0 {
		t.Fatalf("wait past the floor = %v, want 0", got)
	}
}

// newTestSimulator returns a simulator whose clocks are driven by clock
// instead of the wall clock.
func newTestSimulator(speed int, clock *time.Time) *Simulator {
	sim := NewSimulator(nil, nil, speed)
	sim.now = func() time.Time { return *clock }
	sim.simAnchor = *clock
	sim.realAnchor = *clock
	return sim
}

func TestSimulatedClockScalesWithSpeed(t *testing.T) {
	start := time.Date(2024, 1, 15, 8, 0, 0, 0, time.Local)
	clock := start

	sim := newTestSimulator(60, &clock)

	clock = start.Add(1 * time.Minute)
	if got, want := sim.SimulatedNow(), start.Add(1*time.Hour); !got.Equal(want) {
		t.Fatalf("after 1 real minute at 60x: simulated %v, want %v", got, want)
	}

	clock = start.Add(2 * time.Minute)
	if got, want := sim.SimulatedNow(), start.Add(2*time.Hour); !got.Equal(want) {
		t.Fatalf("after 2 real minutes at 60x: simulated %v, want %v", got, want)
	}
}

// TestSetSpeedKeepsSimulatedClockContinuous makes sure a speed change only
// affects how fast time runs from that point on. Replaying the already
// simulated part of the day at the new rate would make the clock jump.
func TestSetSpeedKeepsSimulatedClockContinuous(t *testing.T) {
	start := time.Date(2024, 1, 15, 8, 0, 0, 0, time.Local)
	clock := start

	sim := newTestSimulator(60, &clock)

	clock = start.Add(1 * time.Minute)
	atSwitch := sim.SimulatedNow()

	sim.SetSpeed(120)
	if got := sim.SimulatedNow(); !got.Equal(atSwitch) {
		t.Fatalf("changing speed moved the clock to %v, want it to stay at %v", got, atSwitch)
	}

	clock = clock.Add(1 * time.Minute)
	if got, want := sim.SimulatedNow(), atSwitch.Add(2*time.Hour); !got.Equal(want) {
		t.Fatalf("after 1 real minute at 120x: simulated %v, want %v", got, want)
	}
}

// TestSetSpeedToRealTimeResyncsClock covers the mode that actually guards a
// home: at 1x the routine has to follow the real time of day again, however
// far a fast-forward ran the simulated clock ahead.
func TestSetSpeedToRealTimeResyncsClock(t *testing.T) {
	start := time.Date(2024, 1, 15, 8, 0, 0, 0, time.Local)
	clock := start

	sim := newTestSimulator(1440, &clock)

	clock = start.Add(1 * time.Minute)
	if sim.SimulatedNow().Equal(clock) {
		t.Fatal("expected the simulated clock to run ahead at 1440x")
	}

	sim.SetSpeed(1)
	if got := sim.SimulatedNow(); !got.Equal(clock) {
		t.Fatalf("after returning to 1x: simulated %v, want real time %v", got, clock)
	}

	clock = clock.Add(5 * time.Minute)
	if got := sim.SimulatedNow(); !got.Equal(clock) {
		t.Fatalf("at 1x the clock drifted to %v, want %v", got, clock)
	}
}

// TestStepIgnoresCommunicationErrors makes sure a 207 Multi-Status response
// from the bridge is treated as a transient delivery problem instead of a
// logged application error.
func TestStepIgnoresCommunicationErrors(t *testing.T) {
	var updates atomic.Int32

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/clip/v2/resource/light":
			_, _ = w.Write([]byte(`{"data":[{"id":"light-1","type":"light","metadata":{"name":"Test lamp"},"on":{"on":false},"dimming":{"brightness":50}}]}`))

		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/clip/v2/resource/light/"):
			updates.Add(1)
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = w.Write([]byte(`{"data":[{"rid":"light-1","rtype":"light"}],"errors":[{"description":"device (light) light-1 has communication issues, command (.on.on) may not have effect","error_code":"communication_error"}]}`))

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	bridge := strings.TrimPrefix(server.URL, "https://")
	home, err := openhue.NewHome(bridge, "test-key", openhue.WithCustomHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("create home client: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	sim := NewSimulator(home, storage.NewStore(cfgPath), 1)
	// Pin the clock inside the default "workday" schedule's evening window
	// (20:15 +/-10min jitter, 165min +/-5min duration) so the light is
	// reliably desired "on" regardless of when the test runs.
	sim.now = func() time.Time { return time.Date(2024, 1, 15, 21, 30, 0, 0, time.Local) }

	var logs bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousOutput)

	// The simulator randomly decides whether the lamp should be on. The fake
	// lamp is always off, so a few steps are enough to force an update.
	for i := 0; i < 20 && updates.Load() == 0; i++ {
		sim.step(context.Background())
	}

	if updates.Load() == 0 {
		t.Fatal("expected the simulator to attempt an update")
	}
	if strings.Contains(logs.String(), "Error updating light") {
		t.Fatalf("communication issue was logged as an error:\n%s", logs.String())
	}
}

// TestRunIsSafeUnderConcurrentAccess exercises the loop against the status
// API and the speed slider at once, which is exactly what the UI does. Run
// with -race.
func TestRunIsSafeUnderConcurrentAccess(t *testing.T) {
	bridge := &fakeBridge{}
	sim := NewSimulator(bridge.start(t), writeTestConfig(t, "workday"), 1440)

	previousOutput := log.Writer()
	log.SetOutput(&bytes.Buffer{})
	defer log.SetOutput(previousOutput)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var running sync.WaitGroup
	running.Add(1)
	go func() {
		defer running.Done()
		sim.Run(ctx)
	}()

	deadline := time.Now().Add(300 * time.Millisecond)
	for speed := 1; time.Now().Before(deadline); speed = speed%1440 + 137 {
		sim.SetSpeed(speed)
		_ = sim.Snapshot()
		_ = sim.SimulatedNow()
	}

	cancel()
	running.Wait()

	if snapshot := sim.Snapshot(); snapshot.Steps == 0 {
		t.Fatal("expected the loop to have taken at least one step")
	}
}

// TestRoomKindByLightUsesEveryHint covers the classification chain against
// the shapes a real bridge produces: rooms point at devices, zones point
// straight at lights, and a lamp can be described by any of them.
func TestRoomKindByLightUsesEveryHint(t *testing.T) {
	device := func(id, lightID string) openhue.DeviceGet {
		return openhue.DeviceGet{
			Id:       id,
			Services: []openhue.ResourceIdentifier{{Rid: lightID, Rtype: openhue.ResourceIdentifierRtypeLight}},
		}
	}
	group := func(name, archetype string, children ...openhue.ResourceIdentifier) openhue.RoomGet {
		room := openhue.RoomGet{Children: children}
		room.Metadata.Name = name
		room.Metadata.Archetype = openhue.RoomArchetype(archetype)
		return room
	}
	light := func(name string) openhue.LightGet {
		created := openhue.LightGet{}
		created.Metadata.Name = name
		return created
	}
	viaDevice := func(id string) openhue.ResourceIdentifier {
		return openhue.ResourceIdentifier{Rid: id, Rtype: openhue.ResourceIdentifierRtypeDevice}
	}
	viaLight := func(id string) openhue.ResourceIdentifier {
		return openhue.ResourceIdentifier{Rid: id, Rtype: openhue.ResourceIdentifierRtypeLight}
	}

	devices := map[string]openhue.DeviceGet{
		"dev-archetype": device("dev-archetype", "light-archetype"),
		"dev-name":      device("dev-name", "light-name"),
		"dev-zone":      device("dev-zone", "light-zone"),
		"dev-lamp":      device("dev-lamp", "light-lamp"),
	}
	rooms := map[string]openhue.RoomGet{
		// Archetype and name disagree; the archetype has to win.
		"room-archetype": group("Wohnzimmer", "office", viaDevice("dev-archetype")),
		"room-name":      group("Arbeitszimmer", "other", viaDevice("dev-name")),
		"room-floor":     group("Erdgeschoss", "home", viaDevice("dev-zone"), viaDevice("dev-lamp")),
	}
	zones := map[string]openhue.RoomGet{
		"zone-office": group("Arbeitszimmer", "other", viaLight("light-zone")),
	}
	lights := map[string]openhue.LightGet{
		"light-archetype": light("Deckenlampe"),
		"light-name":      light("Deckenlampe"),
		"light-zone":      light("Deckenlampe"),
		"light-lamp":      light("Schreibtischlampe"),
		"light-orphan":    light("Nachttischlampe"),
	}

	got := RoomKindByLight(rooms, zones, devices, lights)

	want := map[string]schedules.RoomKind{
		"light-archetype": schedules.Office,  // room archetype
		"light-name":      schedules.Office,  // room name
		"light-zone":      schedules.Office,  // zone name
		"light-lamp":      schedules.Office,  // lamp name
		"light-orphan":    schedules.Bedroom, // lamp name, no room at all
	}
	for id, kind := range want {
		if got[id] != kind {
			t.Errorf("light %s classified as %q, want %q", id, got[id], kind)
		}
	}
}

// fakeBridge is a minimal CLIP v2 bridge with one kitchen lamp, enough for
// the simulator to classify the room and act on the routine.
type fakeBridge struct {
	mu sync.Mutex
	on bool
}

func (b *fakeBridge) state() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.on
}

func (b *fakeBridge) start(t *testing.T) *openhue.Home {
	t.Helper()

	write := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[],"data":` + body + `}`))
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/clip/v2/resource/light":
			b.mu.Lock()
			on := b.on
			b.mu.Unlock()
			write(w, fmt.Sprintf(
				`[{"id":"light-1","type":"light","metadata":{"name":"Küche Decke"},"on":{"on":%t},"dimming":{"brightness":100}}]`, on))

		case r.Method == http.MethodGet && r.URL.Path == "/clip/v2/resource/device":
			write(w, `[{"id":"device-1","type":"device","metadata":{"name":"Küche Decke"},`+
				`"services":[{"rid":"light-1","rtype":"light"}]}]`)

		case r.Method == http.MethodGet && r.URL.Path == "/clip/v2/resource/room":
			write(w, `[{"id":"room-1","type":"room","metadata":{"name":"Küche","archetype":"kitchen"},`+
				`"children":[{"rid":"device-1","rtype":"device"}],"services":[]}]`)

		case r.Method == http.MethodGet && r.URL.Path == "/clip/v2/resource/zone":
			write(w, `[]`)

		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/clip/v2/resource/light/"):
			var body struct {
				On *struct {
					On bool `json:"on"`
				} `json:"on"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if body.On != nil {
				b.mu.Lock()
				b.on = body.On.On
				b.mu.Unlock()
			}
			write(w, `[{"rid":"light-1","rtype":"light"}]`)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	home, err := openhue.NewHome(strings.TrimPrefix(server.URL, "https://"), "test-key",
		openhue.WithCustomHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("create home client: %v", err)
	}
	return home
}

func writeTestConfig(t *testing.T, scheduleID string) *storage.Store {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"simulation_schedule":"`+scheduleID+`"}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return storage.NewStore(path)
}

// TestStepFollowsTheRoutine is the regression test for the simulation looking
// frozen: the routine has to be evaluated against the simulated clock, and a
// step has to act on a lamp that disagrees with it rather than picking one at
// random and giving up when it already matches.
func TestStepFollowsTheRoutine(t *testing.T) {
	tests := []struct {
		name    string
		clock   time.Time
		initial bool
		want    bool
	}{
		{
			// Workday has a kitchen window at 18:15 (+/-10min jitter) lasting
			// 75 minutes, so 19:00 is inside it on any day.
			name:    "switches on during a kitchen window",
			clock:   time.Date(2024, 1, 15, 19, 0, 0, 0, time.Local),
			initial: false,
			want:    true,
		},
		{
			name:    "switches off outside every window",
			clock:   time.Date(2024, 1, 15, 3, 0, 0, 0, time.Local),
			initial: true,
			want:    false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bridge := &fakeBridge{on: test.initial}
			clock := test.clock

			sim := NewSimulator(bridge.start(t), writeTestConfig(t, "workday"), 1)
			sim.now = func() time.Time { return clock }
			sim.simAnchor = clock
			sim.realAnchor = clock

			sim.step(context.Background())

			if got := bridge.state(); got != test.want {
				t.Fatalf("lamp is %v after one step, want %v", got, test.want)
			}

			snapshot := sim.Snapshot()
			if snapshot.Steps != 1 {
				t.Fatalf("recorded %d steps, want 1", snapshot.Steps)
			}
			if len(snapshot.Events) != 1 || snapshot.Events[0].On != test.want {
				t.Fatalf("activity log = %+v, want one entry switching to %v", snapshot.Events, test.want)
			}
			if got := snapshot.Events[0].Room; got != schedules.Kitchen {
				t.Fatalf("event room = %q, want %q", got, schedules.Kitchen)
			}
		})
	}
}

// TestStepIsIdempotentOnceSettled makes sure a settled home produces no
// bridge traffic, which is what lets the step interval be short at high
// simulation speeds.
func TestStepIsIdempotentOnceSettled(t *testing.T) {
	bridge := &fakeBridge{on: false}
	clock := time.Date(2024, 1, 15, 3, 0, 0, 0, time.Local)

	sim := NewSimulator(bridge.start(t), writeTestConfig(t, "workday"), 1)
	sim.now = func() time.Time { return clock }
	sim.simAnchor = clock
	sim.realAnchor = clock

	for i := 0; i < 5; i++ {
		sim.step(context.Background())
	}

	snapshot := sim.Snapshot()
	if snapshot.Changes != 0 {
		t.Fatalf("made %d changes to an already settled home, want 0", snapshot.Changes)
	}
	if snapshot.Steps != 5 {
		t.Fatalf("recorded %d steps, want 5", snapshot.Steps)
	}
}
