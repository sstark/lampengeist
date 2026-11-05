package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/openhue/openhue-go"
	"lampengeist/internal/config"
	"lampengeist/internal/presence"
	"lampengeist/internal/storage"
)

//go:embed templates
var templateFiles embed.FS

var (
	stateMu sync.Mutex
	sim     *presence.Simulator
	store   *storage.Store
	client  *openhue.Home
	cfg     config.Config
	appCtx  context.Context
)

type lightState struct {
	On  bool `json:"on"`
	Bri int  `json:"bri"`
}

type lightResponse struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	State        lightState `json:"state"`
	InSimulation bool       `json:"in_simulation"`
}

type lightGroup struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Lights []lightResponse `json:"lights"`
}

func main() {
	cfg = config.LoadConfig()
	store = storage.NewStore("config.json")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	appCtx = ctx

	// Do not exit when the bridge has not been configured yet: the setup UI
	// needs the web server to discover and pair with a bridge.
	if home, err := newHomeClient(cfg); err != nil {
		log.Printf("Hue bridge not configured yet: %v", err)
	} else {
		client = home
		sim = presence.NewSimulator(client, store, cfg.SimulationSpeed)
		go sim.Run(ctx)
	}

	// Web server setup. The UI is embedded in the binary so it is fully
	// self-contained and needs no files next to the executable.
	templateRoot, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		log.Fatalf("Failed to load embedded templates: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(templateRoot)))

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			stateMu.Lock()
			current := cfg
			stateMu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(current)

		case http.MethodPost:
			if err := parseRequestForm(r); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			stateMu.Lock()
			newCfg := cfg
			stateMu.Unlock()

			newCfg.BridgeIP = r.FormValue("bridge_ip")
			newCfg.Enabled = true

			// Keep the existing token when the field is left blank (the setup
			// form only exposes it for manual configuration).
			if token := r.FormValue("token"); token != "" {
				newCfg.Token = token
			}

			if err := store.Save(newCfg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			stateMu.Lock()
			cfg = newCfg
			stateMu.Unlock()

			if newCfg.Token == "" || newCfg.BridgeIP == "" {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, "Configuration saved. Pair a bridge or enter an API token to finish setup.")
				return
			}

			if err := connectBridge(newCfg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "Configuration saved successfully")

		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/discover", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		type bridgeResponse struct {
			Name string `json:"name"`
			IP   string `json:"ip"`
		}

		bridges := []bridgeResponse{}

		discovery := openhue.NewBridgeDiscovery(openhue.WithTimeout(5 * time.Second))
		bridge, err := discovery.Discover()
		if err != nil {
			// No bridge being found is a normal result, not a server error.
			if !errors.Is(err, openhue.NotFoundError) && !errors.Is(err, openhue.TimeoutError) {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else if bridge != nil {
			name := bridge.Instance
			if name == "" || name == "N/A" {
				name = bridge.HostName
			}
			bridges = append(bridges, bridgeResponse{Name: name, IP: bridge.IpAddress})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(bridges)
	})

	mux.HandleFunc("/api/join", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := parseRequestForm(r); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		bridgeIP := r.FormValue("bridge_ip")
		if bridgeIP == "" {
			http.Error(w, "bridge_ip is required", http.StatusBadRequest)
			return
		}

		auth, err := openhue.NewAuthenticator(bridgeIP)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// The user needs time to press the physical pairing button. Keep
		// retrying on every error (not just "link button not pressed") so
		// transient bridge errors do not abort pairing immediately.
		var token string
		var lastErr error
		deadline := time.Now().Add(60 * time.Second)
		for {
			key, _, err := auth.Authenticate()
			if err == nil {
				token = key
				break
			}

			lastErr = err
			if time.Now().After(deadline) {
				log.Printf("Pairing with %s failed: %v", bridgeIP, lastErr)
				http.Error(w, fmt.Sprintf("pairing failed: %v", lastErr), http.StatusBadGateway)
				return
			}

			log.Printf("Pairing with %s: %v (retrying)", bridgeIP, lastErr)
			time.Sleep(2 * time.Second)
		}

		stateMu.Lock()
		newCfg := cfg
		stateMu.Unlock()

		newCfg.BridgeIP = bridgeIP
		newCfg.Token = token
		newCfg.Enabled = true

		if err := store.Save(newCfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		stateMu.Lock()
		cfg = newCfg
		stateMu.Unlock()

		if err := connectBridge(newCfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"bridge_ip": newCfg.BridgeIP,
			"token":     newCfg.Token,
		})
	})

	mux.HandleFunc("/api/simulation", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var request struct {
			LightIDs []string `json:"light_ids"`
			Enabled  bool     `json:"enabled"`
		}

		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if len(request.LightIDs) == 0 {
			http.Error(w, "light_ids must not be empty", http.StatusBadRequest)
			return
		}

		stateMu.Lock()
		defer stateMu.Unlock()

		newCfg := cfg
		newCfg.SimulationLights = copySimulationLights(cfg.SimulationLights)

		changed := false
		for _, lightID := range request.LightIDs {
			if lightID == "" {
				continue
			}
			newCfg.SimulationLights[lightID] = request.Enabled
			changed = true
		}
		if !changed {
			http.Error(w, "light_ids must not be empty", http.StatusBadRequest)
			return
		}
		if len(newCfg.SimulationLights) == 0 {
			newCfg.SimulationLights = nil
		}

		if err := store.Save(newCfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		cfg = newCfg

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":                true,
			"simulation_lights": newCfg.SimulationLights,
		})
	})

	mux.HandleFunc("/api/speed", func(w http.ResponseWriter, r *http.Request) {
		type speedResponse struct {
			Speed int `json:"simulation_speed"`
		}

		switch r.Method {
		case http.MethodGet:
			stateMu.Lock()
			currentSim := sim
			speed := config.ClampSimulationSpeed(cfg.SimulationSpeed)
			stateMu.Unlock()

			if currentSim != nil {
				speed = currentSim.GetSpeed()
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(speedResponse{Speed: speed})

		case http.MethodPost:
			var request struct {
				Speed int `json:"speed"`
			}

			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
			if err := decoder.Decode(&request); err != nil {
				http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
				return
			}

			speed := config.ClampSimulationSpeed(request.Speed)

			stateMu.Lock()
			newCfg := cfg
			newCfg.SimulationSpeed = speed
			if err := store.Save(newCfg); err != nil {
				stateMu.Unlock()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			cfg = newCfg
			currentSim := sim
			stateMu.Unlock()

			if currentSim != nil {
				currentSim.SetSpeed(speed)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(speedResponse{Speed: speed})

		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		currentClient := client
		currentSim := sim
		simulationLights := copySimulationLights(cfg.SimulationLights)
		simulationSpeed := config.ClampSimulationSpeed(cfg.SimulationSpeed)
		stateMu.Unlock()

		lights := []lightResponse{}
		rooms := []lightGroup{}
		zones := []lightGroup{}

		if currentClient != nil {
			var err error
			lights, rooms, zones, err = buildLightGroups(r.Context(), currentClient, simulationLights)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
		}

		var nextEvent *time.Time
		if currentSim != nil {
			t := currentSim.GetNextEvent()
			nextEvent = &t
			simulationSpeed = currentSim.GetSpeed()
		}

		status := struct {
			Lights          []lightResponse `json:"lights"`
			Rooms           []lightGroup    `json:"rooms"`
			Zones           []lightGroup    `json:"zones"`
			NextEvent       *time.Time      `json:"next_event"`
			Configured      bool            `json:"configured"`
			SimulationSpeed int             `json:"simulation_speed"`
		}{
			Lights:          lights,
			Rooms:           rooms,
			Zones:           zones,
			NextEvent:       nextEvent,
			Configured:      currentClient != nil,
			SimulationSpeed: simulationSpeed,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	srv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	// Handle graceful shutdown
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()
	log.Println("Server started on :8080")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop

	log.Println("Shutting down...")
	cancel()
	srv.Shutdown(context.Background())
}

// buildLightGroups fetches all resources needed to show lights grouped by room
// and zone. Everything is sorted deterministically because openhue-go returns
// maps, which do not preserve the order used in the Hue app.
func buildLightGroups(ctx context.Context, home *openhue.Home, simulationLights map[string]bool) ([]lightResponse, []lightGroup, []lightGroup, error) {
	lightsMap, err := home.GetLights(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	devicesMap, err := home.GetDevices(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	roomsMap, err := home.GetRooms(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	zonesMap, err := home.GetZones(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	// Index every light once so groups can share the same values.
	lightsByID := make(map[string]lightResponse, len(lightsMap))
	allLights := make([]lightResponse, 0, len(lightsMap))
	for id, light := range lightsMap {
		response := toLightResponse(id, light, simulationLights)
		lightsByID[id] = response
		allLights = append(allLights, response)
	}
	sortLightsByName(allLights)

	// Rooms reference devices, while zones reference lights directly. Map each
	// device to the light services it exposes before walking the groups.
	deviceLights := make(map[string][]string, len(devicesMap))
	for deviceID, device := range devicesMap {
		for _, service := range device.Services {
			if service.Rtype == openhue.ResourceIdentifierRtypeLight {
				deviceLights[deviceID] = append(deviceLights[deviceID], service.Rid)
			}
		}
	}

	collectGroups := func(groups map[string]openhue.RoomGet) []lightGroup {
		result := make([]lightGroup, 0, len(groups))

		for groupID, group := range groups {
			groupLights := []lightResponse{}
			seen := make(map[string]bool)

			for _, child := range group.Children {
				var lightIDs []string
				switch child.Rtype {
				case openhue.ResourceIdentifierRtypeDevice:
					lightIDs = deviceLights[child.Rid]
				case openhue.ResourceIdentifierRtypeLight:
					lightIDs = []string{child.Rid}
				}

				for _, lightID := range lightIDs {
					light, ok := lightsByID[lightID]
					if !ok || seen[lightID] {
						continue
					}
					seen[lightID] = true
					groupLights = append(groupLights, light)
				}
			}

			if len(groupLights) == 0 {
				continue
			}

			sortLightsByName(groupLights)
			result = append(result, lightGroup{
				ID:     groupID,
				Name:   group.Metadata.Name,
				Lights: groupLights,
			})
		}

		sortGroupsByName(result)
		return result
	}

	rooms := collectGroups(roomsMap)
	zones := collectGroups(zonesMap)

	// Keep lamps that are not part of any room/zone reachable from the UI.
	if len(rooms) > 0 {
		rooms = appendOtherLights(rooms, allLights)
	}
	if len(zones) > 0 {
		zones = appendOtherLights(zones, allLights)
	}

	// Fall back to a single group when the bridge does not define rooms or
	// zones. The flat lights list is still returned for older clients.
	if len(rooms) == 0 {
		rooms = []lightGroup{{ID: "all", Name: "All lamps", Lights: allLights}}
	}
	if len(zones) == 0 {
		zones = []lightGroup{{ID: "all", Name: "All lamps", Lights: allLights}}
	}

	return allLights, rooms, zones, nil
}

func toLightResponse(id string, light openhue.LightGet, simulationLights map[string]bool) lightResponse {
	bri := 0
	if light.Dimming != nil && light.Dimming.Brightness != nil {
		// Hue's v2 API reports brightness as a percentage (0-100).
		bri = int(*light.Dimming.Brightness)
		if bri < 0 {
			bri = 0
		}
		if bri > 100 {
			bri = 100
		}
	}

	return lightResponse{
		ID:   id,
		Name: light.Metadata.Name,
		State: lightState{
			On:  light.On.On,
			Bri: bri,
		},
		InSimulation: config.IsLightInSimulation(simulationLights, id),
	}
}

func appendOtherLights(groups []lightGroup, allLights []lightResponse) []lightGroup {
	grouped := make(map[string]bool)
	for _, group := range groups {
		for _, light := range group.Lights {
			grouped[light.ID] = true
		}
	}

	other := []lightResponse{}
	for _, light := range allLights {
		if !grouped[light.ID] {
			other = append(other, light)
		}
	}

	if len(other) == 0 {
		return groups
	}

	return append(groups, lightGroup{
		ID:     "other",
		Name:   "Other lights",
		Lights: other,
	})
}

func sortLightsByName(lights []lightResponse) {
	sort.SliceStable(lights, func(i, j int) bool {
		// Lamps excluded from the simulation always sort after active ones.
		if lights[i].InSimulation != lights[j].InSimulation {
			return lights[i].InSimulation
		}

		left := strings.ToLower(lights[i].Name)
		right := strings.ToLower(lights[j].Name)
		if left != right {
			return left < right
		}
		return lights[i].ID < lights[j].ID
	})
}

func sortGroupsByName(groups []lightGroup) {
	sort.SliceStable(groups, func(i, j int) bool {
		left := strings.ToLower(groups[i].Name)
		right := strings.ToLower(groups[j].Name)
		if left != right {
			return left < right
		}
		return groups[i].ID < groups[j].ID
	})
}

func copySimulationLights(settings map[string]bool) map[string]bool {
	result := make(map[string]bool, len(settings))
	for id, enabled := range settings {
		result[id] = enabled
	}
	return result
}

// parseRequestForm parses both URL-encoded and multipart form bodies. Browsers
// send FormData as multipart/form-data, which ParseForm alone does not read.
func parseRequestForm(r *http.Request) error {
	if err := r.ParseMultipartForm(32 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return err
	}
	return nil
}

// newHomeClient returns a Hue client only when both credentials are present.
func newHomeClient(c config.Config) (*openhue.Home, error) {
	if c.BridgeIP == "" || c.Token == "" {
		return nil, errors.New("bridge IP and API token must be set")
	}
	return openhue.NewHome(c.BridgeIP, c.Token)
}

// connectBridge swaps the client used by the HTTP handlers and makes sure the
// simulator is running (it may not have been started when the app booted
// without a configuration).
func connectBridge(c config.Config) error {
	home, err := newHomeClient(c)
	if err != nil {
		return err
	}

	stateMu.Lock()
	defer stateMu.Unlock()

	client = home
	if sim == nil {
		sim = presence.NewSimulator(home, store, c.SimulationSpeed)
		go sim.Run(appCtx)
	} else {
		sim.UpdateClient(home)
	}

	return nil
}
