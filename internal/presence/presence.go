package presence

import (
	"context"
	"github.com/openhue/openhue-go"
	"lampengeist/internal/config"
	"lampengeist/internal/storage"
	"log"
	"math/rand"
	"sync"
	"time"
)

type Simulator struct {
	client    *openhue.Home
	store     *storage.Store
	mu        sync.RWMutex
	NextEvent time.Time
}

func NewSimulator(client *openhue.Home, store *storage.Store) *Simulator {
	return &Simulator{
		client:    client,
		store:     store,
		NextEvent: time.Now().Add(1 * time.Minute),
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

func (s *Simulator) Run(ctx context.Context) {
	for {
		// Schedule the next event first so GetNextEvent always reports the
		// actual time the simulator will act.
		delay := time.Duration(rand.Intn(4)+1) * time.Minute
		s.mu.Lock()
		s.NextEvent = time.Now().Add(delay)
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		s.step(ctx)
	}
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
	if err := currentClient.UpdateLight(ctx, lightKey, openhue.LightPut{
		On:      &openhue.On{On: isOn},
		Dimming: &openhue.Dimming{Brightness: 254},
	}); err != nil {
		log.Printf("Error updating light %s: %v", lightKey, err)
	}
}
