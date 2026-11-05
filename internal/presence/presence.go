package presence

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/openhue/openhue-go"
	"lampengeist/internal/config"
	"lampengeist/internal/schedules"
	"lampengeist/internal/storage"
)

// roomCacheTTL bounds how long a room-to-light classification is reused
// before being re-fetched. Room layouts rarely change, and re-fetching on
// every step would hammer the bridge at high simulation speeds.
const roomCacheTTL = 30 * time.Second

// eventLogSize is how many recent lamp changes are kept for the UI.
const eventLogSize = 12

// Event is one lamp change the simulator applied.
type Event struct {
	At          time.Time
	SimulatedAt time.Time
	LightID     string
	LightName   string
	Room        schedules.RoomKind
	On          bool
}

// Snapshot is a consistent view of the simulator's clock and recent work.
type Snapshot struct {
	Speed              int
	SimulatedNow       time.Time
	NextEvent          time.Time
	NextEventSimulated time.Time
	NextEventIn        time.Duration
	Steps              int
	Changes            int
	Events             []Event
}

type Simulator struct {
	client *openhue.Home
	store  *storage.Store

	mu                 sync.RWMutex
	speed              int
	nextEvent          time.Time
	nextEventSimulated time.Time

	// The simulated clock is stored as an anchor pair instead of an
	// accumulated offset: simulated time at realAnchor was simAnchor, and it
	// advances speed times faster than real time from there. Re-anchoring on
	// every speed change keeps the clock continuous.
	simAnchor  time.Time
	realAnchor time.Time

	events  []Event
	steps   int
	changes int

	speedChanged chan struct{}

	// roomCache and roomCacheAt are only touched from the single goroutine
	// running Run/step, so they need no locking of their own.
	roomCache   map[string]schedules.RoomKind
	roomCacheAt time.Time

	// now is overridden in tests so schedule evaluation does not depend on
	// the wall clock at the moment the test happens to run.
	now func() time.Time
}

func NewSimulator(client *openhue.Home, store *storage.Store, speed int) *Simulator {
	start := time.Now()
	return &Simulator{
		client:       client,
		store:        store,
		speed:        config.ClampSimulationSpeed(speed),
		nextEvent:    start.Add(1 * time.Minute),
		simAnchor:    start,
		realAnchor:   start,
		speedChanged: make(chan struct{}, 1),
		now:          time.Now,
	}
}

func (s *Simulator) UpdateClient(client *openhue.Home) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client = client
}

func (s *Simulator) GetSpeed() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.speed
}

// SimulatedNow returns the clock the routine is evaluated against. At 1x it
// is the wall clock; at higher speeds it runs ahead so a whole day's routine
// can be watched in minutes.
func (s *Simulator) SimulatedNow() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.simulatedAtLocked(s.now())
}

// Snapshot returns the simulator state behind the status API.
func (s *Simulator) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := s.now()
	events := make([]Event, len(s.events))
	copy(events, s.events)

	return Snapshot{
		Speed:              s.speed,
		SimulatedNow:       s.simulatedAtLocked(now),
		NextEvent:          s.nextEvent,
		NextEventSimulated: s.nextEventSimulated,
		NextEventIn:        s.nextEvent.Sub(now),
		Steps:              s.steps,
		Changes:            s.changes,
		Events:             events,
	}
}

// simulatedAtLocked projects a real timestamp onto the simulated clock.
func (s *Simulator) simulatedAtLocked(now time.Time) time.Time {
	elapsed := now.Sub(s.realAnchor)
	return s.simAnchor.Add(time.Duration(float64(elapsed) * float64(s.speed)))
}

// SetSpeed changes the simulation speed factor (1x to 1440x). The running
// loop is notified so it can reschedule the current wait immediately.
func (s *Simulator) SetSpeed(speed int) {
	speed = config.ClampSimulationSpeed(speed)

	s.mu.Lock()
	changed := s.speed != speed
	if changed {
		now := s.now()
		// Re-anchor before switching: the time already simulated keeps the
		// rate it was simulated at instead of being retroactively replayed.
		s.simAnchor = s.simulatedAtLocked(now)
		s.realAnchor = now
		s.speed = speed

		// 1x is the mode that actually guards a home, so returning to it has
		// to line the simulated clock back up with real time. Otherwise a
		// fast-forward would leave the routine running hours off, switching
		// lamps on in the middle of the night.
		if speed == config.MinSimulationSpeed {
			s.simAnchor = now
		}
	}
	s.mu.Unlock()

	if changed {
		select {
		case s.speedChanged <- struct{}{}:
		default:
		}
	}
}

func (s *Simulator) Run(ctx context.Context) {
	lastStep := s.now()

	for {
		// The next step is due one to four simulated minutes from now. Keeping
		// the deadline on the simulated clock means a speed change needs no
		// bookkeeping: it only changes how fast the clock reaches it.
		due := s.SimulatedNow().Add(time.Duration(rand.Intn(4)+1) * time.Minute)

		if !s.waitUntil(ctx, due, lastStep) {
			return
		}

		s.step(ctx)
		lastStep = s.now()
	}
}

// waitUntil blocks until the simulated clock reaches due, recomputing the
// wait whenever the speed changes. It reports false if the context ended.
func (s *Simulator) waitUntil(ctx context.Context, due, lastStep time.Time) bool {
	for {
		timer := time.NewTimer(s.scheduleNext(due, lastStep))

		select {
		case <-ctx.Done():
			timer.Stop()
			return false

		case <-s.speedChanged:
			if !timer.Stop() {
				<-timer.C
			}

		case <-timer.C:
			return true
		}
	}
}

// scheduleNext records when the next step is due and returns how long to wait
// for it in real time.
func (s *Simulator) scheduleNext(due, lastStep time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	simulatedNow := s.simulatedAtLocked(now)
	realDelay := scaledDelay(due.Sub(simulatedNow), s.speed)

	// Space steps out so the highest speed factors cannot flood the bridge.
	// The floor is measured from the last step rather than from the pending
	// wait, so a stream of speed changes cannot postpone a step forever.
	if earliest := lastStep.Add(minStepSpacing); now.Add(realDelay).Before(earliest) {
		realDelay = max(earliest.Sub(now), 0)
	}

	s.nextEvent = now.Add(realDelay)
	// Derived from the real delay so both clocks tell the same story: the
	// spacing floor can push the step past the simulated time it was due.
	s.nextEventSimulated = simulatedNow.Add(time.Duration(float64(realDelay) * float64(s.speed)))

	return realDelay
}

// minStepSpacing is the shortest real time allowed between two steps.
const minStepSpacing = 100 * time.Millisecond

// scaledDelay converts a simulated duration into real time at the given speed.
func scaledDelay(simulated time.Duration, speed int) time.Duration {
	speed = config.ClampSimulationSpeed(speed)
	return max(time.Duration(float64(simulated)/float64(speed)), 0)
}

func (s *Simulator) step(ctx context.Context) {
	s.mu.RLock()
	currentClient := s.client
	s.mu.RUnlock()

	if currentClient == nil {
		log.Println("Simulator: client is nil")
		return
	}

	lights, err := currentClient.GetLights(ctx)
	if err != nil {
		log.Printf("Error getting lights: %v", err)
		return
	}

	if len(lights) == 0 {
		return
	}

	// Lamp settings are persisted in config.json. Reload them before each
	// step so toggles made in the UI take effect without a restart.
	simulationLights := map[string]bool{}
	scheduleID := schedules.DefaultID
	if s.store != nil {
		storedConfig, err := s.store.Load()
		if err != nil {
			log.Printf("Simulator: error loading config: %v", err)
		} else {
			simulationLights = storedConfig.SimulationLights
			scheduleID = storedConfig.SimulationSchedule
		}
	}

	keys := make([]string, 0, len(lights))
	for k := range lights {
		if !config.IsLightInSimulation(simulationLights, k) {
			continue
		}
		keys = append(keys, k)
	}

	if len(keys) == 0 {
		log.Println("Simulator: no lamps enabled for simulation")
		return
	}
	sort.Strings(keys)

	roomOfLight := s.roomClassification(ctx, currentClient)
	active := schedules.Get(scheduleID).ActiveAt(s.SimulatedNow())
	desiredOn := schedules.DesiredStates(active, roomOfLight, keys)

	// Only consider lamps that disagree with the routine. Picking a lamp at
	// random and giving up when it already matches made most steps no-ops, so
	// the simulation crawled towards the routine (or looked frozen) instead of
	// following it.
	pending := make([]string, 0, len(keys))
	for _, key := range keys {
		candidate := lights[key]
		if candidate.IsOn() != desiredOn[key] {
			pending = append(pending, key)
		}
	}

	s.recordStep()
	if len(pending) == 0 {
		return
	}

	// Still one lamp per step: a whole room coming on at once does not look
	// like someone walking through a home, and it spikes bridge traffic.
	lightKey := pending[rand.Intn(len(pending))]
	light := lights[lightKey]
	isOn := desiredOn[lightKey]

	log.Printf("Toggling light %s to %v", lightKey, isOn)

	update := openhue.LightPut{
		On: &openhue.On{On: isOn},
	}
	// Only dimmable lights accept a brightness value, and only bother when
	// switching a lamp on. Hue's v2 API expresses brightness as a percentage
	// (0-100), not the 0-254 range used by v1.
	if isOn && light.Dimming != nil {
		update.Dimming = &openhue.Dimming{Brightness: 100}
	}

	if err := currentClient.UpdateLight(ctx, lightKey, update); err != nil {
		// 207 Multi-Status means the bridge accepted the request but could not
		// reach the device (for example a bulb in standby). The command is
		// retried on a later run, so this is not an application error.
		if isTransientUpdateError(err) {
			return
		}
		log.Printf("Error updating light %s: %v", lightKey, err)
		return
	}

	s.recordEvent(Event{
		LightID:   lightKey,
		LightName: light.Metadata.Name,
		Room:      roomOfLight[lightKey],
		On:        isOn,
	})
}

// recordStep counts an evaluation of the routine, so the UI can show the
// simulator is alive even while it has nothing to change.
func (s *Simulator) recordStep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps++
}

// recordEvent appends a lamp change to the bounded activity log, stamping it
// with both clocks.
func (s *Simulator) recordEvent(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	event.At = s.now()
	event.SimulatedAt = s.simulatedAtLocked(event.At)

	s.changes++
	s.events = append(s.events, event)
	if len(s.events) > eventLogSize {
		s.events = s.events[1:]
	}
}

// isTransientUpdateError reports whether the bridge accepted a command but
// could not deliver it to the device. Those updates are retried later.
func isTransientUpdateError(err error) bool {
	var apiErr *openhue.ApiError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusMultiStatus
}

// roomClassification returns the cached room-kind lookup, refreshing it from
// the bridge when stale. A failed refresh keeps serving the previous cache
// (or nil on the very first failure) rather than aborting the step.
func (s *Simulator) roomClassification(ctx context.Context, client *openhue.Home) map[string]schedules.RoomKind {
	if s.roomCache != nil && time.Since(s.roomCacheAt) < roomCacheTTL {
		return s.roomCache
	}

	classified, err := ClassifyLightsByRoom(ctx, client)
	if err != nil {
		log.Printf("Simulator: error classifying rooms: %v", err)
		return s.roomCache
	}

	s.roomCache = classified
	s.roomCacheAt = time.Now()
	return s.roomCache
}

// ClassifyLightsByRoom maps every light ID to a routine room category.
func ClassifyLightsByRoom(ctx context.Context, client *openhue.Home) (map[string]schedules.RoomKind, error) {
	rooms, err := client.GetRooms(ctx)
	if err != nil {
		return nil, err
	}

	zones, err := client.GetZones(ctx)
	if err != nil {
		return nil, err
	}

	devices, err := client.GetDevices(ctx)
	if err != nil {
		return nil, err
	}

	lights, err := client.GetLights(ctx)
	if err != nil {
		return nil, err
	}

	return RoomKindByLight(rooms, zones, devices, lights), nil
}

// RoomKindByLight builds the light-to-room-category lookup from already
// fetched bridge resources. Rooms reference devices rather than lights
// directly, so each group's devices are first resolved to the light services
// they expose.
//
// Zones and lamp names are collected as well, because a Hue setup is only as
// descriptive as its owner made it: rooms named by floor say nothing, while
// the zone or the lamp often does.
func RoomKindByLight(rooms, zones map[string]openhue.RoomGet, devices map[string]openhue.DeviceGet, lights map[string]openhue.LightGet) map[string]schedules.RoomKind {
	deviceLights := make(map[string][]string, len(devices))
	for deviceID, device := range devices {
		for _, service := range device.Services {
			if service.Rtype == openhue.ResourceIdentifierRtypeLight {
				deviceLights[deviceID] = append(deviceLights[deviceID], service.Rid)
			}
		}
	}

	groupLights := func(group openhue.RoomGet) []string {
		lightIDs := []string{}
		for _, child := range group.Children {
			switch child.Rtype {
			case openhue.ResourceIdentifierRtypeDevice:
				lightIDs = append(lightIDs, deviceLights[child.Rid]...)
			case openhue.ResourceIdentifierRtypeLight:
				lightIDs = append(lightIDs, child.Rid)
			}
		}
		return lightIDs
	}

	hints := make(map[string]*schedules.Hints, len(lights))
	hintsFor := func(lightID string) *schedules.Hints {
		if existing, ok := hints[lightID]; ok {
			return existing
		}
		created := &schedules.Hints{}
		hints[lightID] = created
		return created
	}

	for _, room := range sortedGroups(rooms) {
		for _, lightID := range groupLights(room) {
			lightHints := hintsFor(lightID)
			lightHints.RoomArchetype = string(room.Metadata.Archetype)
			lightHints.RoomName = room.Metadata.Name
		}
	}

	// Sorted so a lamp that belongs to several zones always consults them in
	// the same order, and therefore always lands in the same category.
	for _, zone := range sortedGroups(zones) {
		for _, lightID := range groupLights(zone) {
			lightHints := hintsFor(lightID)
			lightHints.ZoneArchetypes = append(lightHints.ZoneArchetypes, string(zone.Metadata.Archetype))
			lightHints.ZoneNames = append(lightHints.ZoneNames, zone.Metadata.Name)
		}
	}

	for lightID, light := range lights {
		hintsFor(lightID).LampName = light.Metadata.Name
	}

	roomOfLight := make(map[string]schedules.RoomKind, len(hints))
	for lightID, lightHints := range hints {
		roomOfLight[lightID] = schedules.Classify(*lightHints)
	}
	return roomOfLight
}

func sortedGroups(groups map[string]openhue.RoomGet) []openhue.RoomGet {
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	sorted := make([]openhue.RoomGet, 0, len(groups))
	for _, id := range ids {
		sorted = append(sorted, groups[id])
	}
	return sorted
}
