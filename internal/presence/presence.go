package presence

import (
	"context"
	"errors"
	"github.com/openhue/openhue-go"
	"lampengeist/internal/config"
	"lampengeist/internal/schedules"
	"lampengeist/internal/storage"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// roomCacheTTL bounds how long a room-to-light classification is reused
// before being re-fetched. Room layouts rarely change, and re-fetching on
// every step would hammer the bridge at high simulation speeds.
const roomCacheTTL = 30 * time.Second

type Simulator struct {
	client       *openhue.Home
	store        *storage.Store
	mu           sync.RWMutex
	NextEvent    time.Time
	speed        int
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
	return &Simulator{
		client:       client,
		store:        store,
		NextEvent:    time.Now().Add(1 * time.Minute),
		speed:        config.ClampSimulationSpeed(speed),
		speedChanged: make(chan struct{}, 1),
		now:          time.Now,
	}
}

func (s *Simulator) UpdateClient(client *openhue.Home) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client = client
}

func (s *Simulator) GetNextEvent() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.NextEvent
}

func (s *Simulator) GetSpeed() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.speed
}

// SetSpeed changes the simulation speed factor (1x to 1440x). The running
// loop is notified so it can reschedule the current wait immediately.
func (s *Simulator) SetSpeed(speed int) {
	speed = config.ClampSimulationSpeed(speed)

	s.mu.Lock()
	changed := s.speed != speed
	s.speed = speed
	s.mu.Unlock()

	if changed {
		select {
		case s.speedChanged <- struct{}{}:
		default:
		}
	}
}

func (s *Simulator) Run(ctx context.Context) {
	for {
		remainingSimulated := time.Duration(rand.Intn(4)+1) * time.Minute

		s.mu.Lock()
		speed := s.speed
		realDelay := scaledDelay(remainingSimulated, speed)
		s.NextEvent = time.Now().Add(realDelay)
		s.mu.Unlock()

		// Wait for the scaled delay, but react immediately when the user
		// changes the speed while the timer is running.
		timer := time.NewTimer(realDelay)
		waitStarted := time.Now()
		waiting := true

		for waiting {
			select {
			case <-ctx.Done():
				timer.Stop()
				return

			case <-s.speedChanged:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}

				now := time.Now()
				elapsedSimulated := time.Duration(float64(now.Sub(waitStarted)) * float64(speed))
				if elapsedSimulated > remainingSimulated {
					elapsedSimulated = remainingSimulated
				}
				remainingSimulated -= elapsedSimulated

				s.mu.Lock()
				speed = s.speed
				realDelay = scaledDelay(remainingSimulated, speed)
				s.NextEvent = now.Add(realDelay)
				s.mu.Unlock()

				waitStarted = now
				timer.Reset(realDelay)

			case <-timer.C:
				waiting = false
			}
		}

		s.step(ctx)
	}
}

// scaledDelay converts a simulated duration into real time. A small floor
// prevents flooding the Hue bridge at the highest speed factors.
func scaledDelay(simulated time.Duration, speed int) time.Duration {
	speed = config.ClampSimulationSpeed(speed)
	realDelay := time.Duration(float64(simulated) / float64(speed))

	const minRealDelay = 100 * time.Millisecond
	if realDelay < minRealDelay {
		return minRealDelay
	}
	return realDelay
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

	desiredOn := s.desiredLightStates(ctx, currentClient, keys, scheduleID)

	lightKey := keys[rand.Intn(len(keys))]
	light := lights[lightKey]

	isOn := desiredOn[lightKey]
	if light.IsOn() == isOn {
		return
	}

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
	}
}

// isTransientUpdateError reports whether the bridge accepted a command but
// could not deliver it to the device. Those updates are retried later.
func isTransientUpdateError(err error) bool {
	var apiErr *openhue.ApiError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusMultiStatus
}

// desiredLightStates returns, for each candidate light, whether the active
// schedule wants it on right now. Lights whose room has no active window are
// absent from the result (and therefore desired off).
func (s *Simulator) desiredLightStates(ctx context.Context, client *openhue.Home, lightIDs []string, scheduleID string) map[string]bool {
	active := schedules.Get(scheduleID).ActiveAt(s.now())
	if len(active) == 0 {
		return map[string]bool{}
	}

	roomOfLight := s.roomClassification(ctx, client)

	lightsByRoom := make(map[schedules.RoomKind][]string)
	for _, id := range lightIDs {
		room, ok := roomOfLight[id]
		if !ok {
			room = schedules.Other
		}
		lightsByRoom[room] = append(lightsByRoom[room], id)
	}

	desired := make(map[string]bool)
	for _, window := range active {
		for _, room := range window.Window.Rooms {
			ids := lightsByRoom[room]
			if len(ids) == 0 {
				continue
			}
			for id := range window.SelectLights(room, ids) {
				desired[id] = true
			}
		}
	}
	return desired
}

// roomClassification returns the cached room-kind lookup, refreshing it from
// the bridge when stale. A failed refresh keeps serving the previous cache
// (or nil on the very first failure) rather than aborting the step.
func (s *Simulator) roomClassification(ctx context.Context, client *openhue.Home) map[string]schedules.RoomKind {
	if s.roomCache != nil && time.Since(s.roomCacheAt) < roomCacheTTL {
		return s.roomCache
	}

	classified, err := classifyLightsByRoom(ctx, client)
	if err != nil {
		log.Printf("Simulator: error classifying rooms: %v", err)
		return s.roomCache
	}

	s.roomCache = classified
	s.roomCacheAt = time.Now()
	return s.roomCache
}

// classifyLightsByRoom maps every light ID to the room category of the Hue
// room it belongs to. Rooms reference devices rather than lights directly, so
// each room's devices are first resolved to the light services they expose.
func classifyLightsByRoom(ctx context.Context, client *openhue.Home) (map[string]schedules.RoomKind, error) {
	rooms, err := client.GetRooms(ctx)
	if err != nil {
		return nil, err
	}

	devices, err := client.GetDevices(ctx)
	if err != nil {
		return nil, err
	}

	deviceLights := make(map[string][]string, len(devices))
	for deviceID, device := range devices {
		for _, service := range device.Services {
			if service.Rtype == openhue.ResourceIdentifierRtypeLight {
				deviceLights[deviceID] = append(deviceLights[deviceID], service.Rid)
			}
		}
	}

	roomOfLight := make(map[string]schedules.RoomKind)
	for _, room := range rooms {
		kind := schedules.ClassifyRoom(room.Metadata.Name)
		for _, child := range room.Children {
			var lightIDs []string
			switch child.Rtype {
			case openhue.ResourceIdentifierRtypeDevice:
				lightIDs = deviceLights[child.Rid]
			case openhue.ResourceIdentifierRtypeLight:
				lightIDs = []string{child.Rid}
			}
			for _, id := range lightIDs {
				roomOfLight[id] = kind
			}
		}
	}
	return roomOfLight, nil
}
