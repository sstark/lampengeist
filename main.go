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
	"lampengeist/internal/schedules"
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
	RoomKind     string     `json:"room_kind"`
	RoomLabel    string     `json:"room_label"`
	DesiredOn    bool       `json:"desired_on"`
	// Pending marks a lamp the routine wants in the other state. The
	// simulator changes one lamp per step, so this is what it is working
	// towards next.
	Pending bool `json:"pending"`
}

type lightGroup struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Lights []lightResponse `json:"lights"`
}

// timelineBlock is one room's slice of one routine window, clipped to the
// simulated day so the UI can lay it out on a 24 hour axis.
type timelineBlock struct {
	RoomKind    string `json:"room_kind"`
	RoomLabel   string `json:"room_label"`
	StartMinute int    `json:"start_minute"`
	EndMinute   int    `json:"end_minute"`
	StartLabel  string `json:"start_label"`
	EndLabel    string `json:"end_label"`
	Active      bool   `json:"active"`
	// HasLights is false when the routine asks for a room the user has no
	// lamp in, which is the usual reason a routine looks like it does nothing.
	HasLights bool `json:"has_lights"`
}

type activityEntry struct {
	LightName   string  `json:"light_name"`
	RoomLabel   string  `json:"room_label"`
	On          bool    `json:"on"`
	SimulatedAt string  `json:"simulated_at"`
	AgeSeconds  float64 `json:"age_seconds"`
}

// simulationStatus describes the simulated clock in units the browser can
// animate without any date or timezone arithmetic of its own: minutes since
// simulated midnight, plus real seconds until the next step.
type simulationStatus struct {
	Running           bool            `json:"running"`
	Speed             int             `json:"speed"`
	SimulatedMinute   float64         `json:"simulated_minute"`
	SimulatedDayLabel string          `json:"simulated_day_label"`
	ClockOffset       string          `json:"clock_offset"`
	NextEventIn       float64         `json:"next_event_in"`
	NextEventMinute   float64         `json:"next_event_minute"`
	Steps             int             `json:"steps"`
	Changes           int             `json:"changes"`
	ActiveRooms       []string        `json:"active_rooms"`
	Timeline          []timelineBlock `json:"timeline"`
	Activity          []activityEntry `json:"activity"`
	LampsOn           int             `json:"lamps_on"`
	LampsSimulated    int             `json:"lamps_simulated"`
	PendingLamps      int             `json:"pending_lamps"`
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

	mux.HandleFunc("/api/schedules", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(schedules.All())
	})

	mux.HandleFunc("/api/schedule", func(w http.ResponseWriter, r *http.Request) {
		type scheduleResponse struct {
			Schedule string `json:"schedule"`
		}

		switch r.Method {
		case http.MethodGet:
			stateMu.Lock()
			current := schedules.Normalize(cfg.SimulationSchedule)
			stateMu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(scheduleResponse{Schedule: current})

		case http.MethodPost:
			var request struct {
				Schedule string `json:"schedule"`
			}

			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
			if err := decoder.Decode(&request); err != nil {
				http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
				return
			}
			if !schedules.Exists(request.Schedule) {
				http.Error(w, "unknown schedule id", http.StatusBadRequest)
				return
			}

			stateMu.Lock()
			newCfg := cfg
			newCfg.SimulationSchedule = request.Schedule
			if err := store.Save(newCfg); err != nil {
				stateMu.Unlock()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			cfg = newCfg
			stateMu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(scheduleResponse{Schedule: request.Schedule})

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
		simulationSchedule := schedules.Normalize(cfg.SimulationSchedule)
		stateMu.Unlock()

		// One snapshot for the whole response, so the clock, the timeline and
		// the per-lamp targets are all read against the same simulated moment.
		var snapshot *presence.Snapshot
		simulatedNow := time.Now()
		if currentSim != nil {
			taken := currentSim.Snapshot()
			snapshot = &taken
			simulatedNow = taken.SimulatedNow
			simulationSpeed = taken.Speed
		}

		view := lightsView{}
		if currentClient != nil {
			var err error
			view, err = buildLightsView(r.Context(), currentClient, simulationLights, simulationSchedule, simulatedNow)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
		}

		simulation := simulationStatus{Speed: simulationSpeed}
		if snapshot != nil {
			simulation = buildSimulationStatus(*snapshot, view, simulationSchedule)
		}

		status := struct {
			Lights             []lightResponse  `json:"lights"`
			Rooms              []lightGroup     `json:"rooms"`
			Zones              []lightGroup     `json:"zones"`
			Configured         bool             `json:"configured"`
			SimulationSpeed    int              `json:"simulation_speed"`
			SimulationSchedule string           `json:"simulation_schedule"`
			Simulation         simulationStatus `json:"simulation"`
		}{
			Lights:             view.lights,
			Rooms:              view.rooms,
			Zones:              view.zones,
			Configured:         currentClient != nil,
			SimulationSpeed:    simulationSpeed,
			SimulationSchedule: simulationSchedule,
			Simulation:         simulation,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	srv := &http.Server{
		Addr:    ":64057",
		Handler: mux,
	}

	// Handle graceful shutdown
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()
	log.Println("Server started on :64057")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop

	log.Println("Shutting down...")
	cancel()
	srv.Shutdown(context.Background())
}

// lightsView is everything /api/status needs about the lamps: the flat list,
// the room and zone groupings, and the routine's current intent.
type lightsView struct {
	lights      []lightResponse
	rooms       []lightGroup
	zones       []lightGroup
	roomOfLight map[string]schedules.RoomKind
	activeNow   []schedules.ActiveWindow
	simulated   []string
	pending     int
	on          int
}

// buildLightsView fetches all resources needed to show lights grouped by room
// and zone, and resolves what the routine wants at simulatedNow. Everything is
// sorted deterministically because openhue-go returns maps, which do not
// preserve the order used in the Hue app.
func buildLightsView(ctx context.Context, home *openhue.Home, simulationLights map[string]bool, scheduleID string, simulatedNow time.Time) (lightsView, error) {
	lightsMap, err := bridgeResources.lights(ctx, home)
	if err != nil {
		return lightsView{}, err
	}

	layout, err := bridgeResources.layout(ctx, home)
	if err != nil {
		return lightsView{}, err
	}
	devicesMap, roomsMap, zonesMap := layout.devices, layout.rooms, layout.zones

	// The routine decides per room, so the lamps have to be classified before
	// the response is built. The simulator classifies them the same way from
	// the same bridge data, which is what keeps the two in agreement.
	roomOfLight := presence.RoomKindByLight(roomsMap, zonesMap, devicesMap, lightsMap)

	simulated := make([]string, 0, len(lightsMap))
	for id := range lightsMap {
		if config.IsLightInSimulation(simulationLights, id) {
			simulated = append(simulated, id)
		}
	}
	sort.Strings(simulated)

	activeNow := schedules.Get(scheduleID).ActiveAt(simulatedNow)
	desiredOn := schedules.DesiredStates(activeNow, roomOfLight, simulated)

	view := lightsView{
		roomOfLight: roomOfLight,
		activeNow:   activeNow,
		simulated:   simulated,
	}

	// Index every light once so groups can share the same values.
	lightsByID := make(map[string]lightResponse, len(lightsMap))
	allLights := make([]lightResponse, 0, len(lightsMap))
	for id, light := range lightsMap {
		response := toLightResponse(id, light, simulationLights, roomOfLight, desiredOn)
		lightsByID[id] = response
		allLights = append(allLights, response)

		if response.State.On {
			view.on++
		}
		if response.Pending {
			view.pending++
		}
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

	view.lights = allLights
	view.rooms = rooms
	view.zones = zones
	return view, nil
}

func toLightResponse(id string, light openhue.LightGet, simulationLights map[string]bool, roomOfLight map[string]schedules.RoomKind, desiredOn map[string]bool) lightResponse {
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

	room, ok := roomOfLight[id]
	if !ok {
		// Unclassified lamps are what the routines call "other rooms".
		room = schedules.Other
	}

	inSimulation := config.IsLightInSimulation(simulationLights, id)
	desired := inSimulation && desiredOn[id]

	return lightResponse{
		ID:   id,
		Name: light.Metadata.Name,
		State: lightState{
			On:  light.On.On,
			Bri: bri,
		},
		InSimulation: inSimulation,
		RoomKind:     string(room),
		RoomLabel:    schedules.RoomLabel(room),
		DesiredOn:    desired,
		Pending:      inSimulation && light.On.On != desired,
	}
}

// buildSimulationStatus renders the simulator state for the UI. Times are
// expressed as minutes since simulated midnight so the browser can animate
// the clock without parsing dates from a day that may not be today.
func buildSimulationStatus(snapshot presence.Snapshot, view lightsView, scheduleID string) simulationStatus {
	simulated := snapshot.SimulatedNow

	status := simulationStatus{
		Running:           true,
		Speed:             snapshot.Speed,
		SimulatedMinute:   minuteOfDay(simulated),
		SimulatedDayLabel: simulated.Format("Mon, 2 Jan"),
		ClockOffset:       describeClockOffset(simulated, time.Now()),
		NextEventIn:       snapshot.NextEventIn.Seconds(),
		NextEventMinute:   minuteOfDay(snapshot.NextEventSimulated),
		Steps:             snapshot.Steps,
		Changes:           snapshot.Changes,
		ActiveRooms:       []string{},
		Timeline:          buildTimeline(scheduleID, simulated, view),
		Activity:          buildActivity(snapshot.Events),
		LampsOn:           view.on,
		LampsSimulated:    len(view.simulated),
		PendingLamps:      view.pending,
	}

	seen := map[schedules.RoomKind]bool{}
	for _, window := range view.activeNow {
		for _, room := range window.Window.Rooms {
			if seen[room] {
				continue
			}
			seen[room] = true
			status.ActiveRooms = append(status.ActiveRooms, schedules.RoomLabel(room))
		}
	}
	sort.Strings(status.ActiveRooms)

	return status
}

// buildTimeline flattens the routine's occurrences for the simulated day into
// one block per room, clipped to the day's 1440 minutes.
func buildTimeline(scheduleID string, simulated time.Time, view lightsView) []timelineBlock {
	dayStart := time.Date(simulated.Year(), simulated.Month(), simulated.Day(), 0, 0, 0, 0, simulated.Location())

	roomsWithLights := map[schedules.RoomKind]bool{}
	for room := range schedules.GroupByRoom(view.roomOfLight, view.simulated) {
		roomsWithLights[room] = true
	}

	blocks := []timelineBlock{}
	for _, occurrence := range schedules.Get(scheduleID).OccurrencesOnDay(simulated) {
		startMinute := clampMinute(occurrence.Start.Sub(dayStart).Minutes())
		endMinute := clampMinute(occurrence.End.Sub(dayStart).Minutes())
		if endMinute <= startMinute {
			continue
		}

		for _, room := range occurrence.Window.Rooms {
			blocks = append(blocks, timelineBlock{
				RoomKind:    string(room),
				RoomLabel:   schedules.RoomLabel(room),
				StartMinute: startMinute,
				EndMinute:   endMinute,
				StartLabel:  occurrence.Start.Format("15:04"),
				EndLabel:    occurrence.End.Format("15:04"),
				Active:      occurrence.Contains(simulated),
				HasLights:   roomsWithLights[room],
			})
		}
	}

	sort.SliceStable(blocks, func(i, j int) bool {
		left := schedules.RoomOrderIndex(schedules.RoomKind(blocks[i].RoomKind))
		right := schedules.RoomOrderIndex(schedules.RoomKind(blocks[j].RoomKind))
		if left != right {
			return left < right
		}
		return blocks[i].StartMinute < blocks[j].StartMinute
	})
	return blocks
}

func buildActivity(events []presence.Event) []activityEntry {
	now := time.Now()
	entries := make([]activityEntry, 0, len(events))

	// Newest first: the log is read top-down.
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		entries = append(entries, activityEntry{
			LightName:   event.LightName,
			RoomLabel:   schedules.RoomLabel(event.Room),
			On:          event.On,
			SimulatedAt: event.SimulatedAt.Format("15:04"),
			AgeSeconds:  now.Sub(event.At).Seconds(),
		})
	}
	return entries
}

// describeClockOffset summarises how far the simulated clock has run from
// real time, which is how a fast-forward shows up to the user.
func describeClockOffset(simulated, real time.Time) string {
	offset := simulated.Sub(real)
	if offset < 0 {
		offset = -offset
	}
	if offset < time.Minute {
		return ""
	}

	switch {
	case offset < time.Hour:
		return fmt.Sprintf("%d min ahead of real time", int(offset.Minutes()))
	case offset < 48*time.Hour:
		return fmt.Sprintf("%.1f h ahead of real time", offset.Hours())
	default:
		return fmt.Sprintf("%.1f days ahead of real time", offset.Hours()/24)
	}
}

func minuteOfDay(t time.Time) float64 {
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return t.Sub(midnight).Minutes()
}

func clampMinute(minute float64) int {
	if minute < 0 {
		return 0
	}
	if minute > 1440 {
		return 1440
	}
	return int(minute)
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

// Bridge reads are cached because every /api/status poll needs four of them,
// and the UI polls once a second at high simulation speeds. Light state has a
// short TTL so the lamp cards stay live; the room layout only changes when the
// user edits their Hue setup.
const (
	lightsCacheTTL = 750 * time.Millisecond
	layoutCacheTTL = 30 * time.Second
)

var bridgeResources = &bridgeCache{}

type bridgeLayout struct {
	rooms   map[string]openhue.RoomGet
	zones   map[string]openhue.RoomGet
	devices map[string]openhue.DeviceGet
}

type bridgeCache struct {
	mu sync.Mutex

	lightsValue map[string]openhue.LightGet
	lightsAt    time.Time

	layoutValue *bridgeLayout
	layoutAt    time.Time
}

func (c *bridgeCache) lights(ctx context.Context, home *openhue.Home) (map[string]openhue.LightGet, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.lightsValue != nil && time.Since(c.lightsAt) < lightsCacheTTL {
		return c.lightsValue, nil
	}

	lights, err := home.GetLights(ctx)
	if err != nil {
		return nil, err
	}

	c.lightsValue = lights
	c.lightsAt = time.Now()
	return lights, nil
}

func (c *bridgeCache) layout(ctx context.Context, home *openhue.Home) (*bridgeLayout, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.layoutValue != nil && time.Since(c.layoutAt) < layoutCacheTTL {
		return c.layoutValue, nil
	}

	devices, err := home.GetDevices(ctx)
	if err != nil {
		return nil, err
	}
	rooms, err := home.GetRooms(ctx)
	if err != nil {
		return nil, err
	}
	zones, err := home.GetZones(ctx)
	if err != nil {
		return nil, err
	}

	c.layoutValue = &bridgeLayout{rooms: rooms, zones: zones, devices: devices}
	c.layoutAt = time.Now()
	return c.layoutValue, nil
}

// invalidate drops the cache so a freshly paired bridge is read immediately.
func (c *bridgeCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lightsValue = nil
	c.layoutValue = nil
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

	bridgeResources.invalidate()

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
