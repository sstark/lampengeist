package presence

import (
	"context"
	"errors"
	"github.com/openhue/openhue-go"
	"lampengeist/internal/config"
	"lampengeist/internal/storage"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

type Simulator struct {
	client       *openhue.Home
	store        *storage.Store
	mu           sync.RWMutex
	NextEvent    time.Time
	speed        int
	speedChanged chan struct{}
}

func NewSimulator(client *openhue.Home, store *storage.Store, speed int) *Simulator {
	return &Simulator{
		client:       client,
		store:        store,
		NextEvent:    time.Now().Add(1 * time.Minute),
		speed:        config.ClampSimulationSpeed(speed),
		speedChanged: make(chan struct{}, 1),
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
	if s.store != nil {
		storedConfig, err := s.store.Load()
		if err != nil {
			log.Printf("Simulator: error loading config: %v", err)
		} else {
			simulationLights = storedConfig.SimulationLights
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

	lightKey := keys[rand.Intn(len(keys))]
	light := lights[lightKey]

	// Semi-random logic: 70% chance the light should be on.
	isOn := rand.Float32() < 0.7
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
