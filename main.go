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
	"strconv"
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
		sim = presence.NewSimulator(client, store)
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

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		currentClient := client
		currentSim := sim
		stateMu.Unlock()

		type lightResponse struct {
			ID    int    `json:"id"`
			Name  string `json:"name"`
			State struct {
				On  bool `json:"on"`
				Bri int  `json:"bri"`
			} `json:"state"`
		}

		lights := []lightResponse{}
		if currentClient != nil {
			lightsMap, err := currentClient.GetLights(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}

			for _, l := range lightsMap {
				id, _ := strconv.Atoi(l.Id)
				bri := 0
				if l.Dimming != nil && l.Dimming.Brightness != nil {
					bri = int(*l.Dimming.Brightness)
				}
				lights = append(lights, lightResponse{
					ID:   id,
					Name: l.Metadata.Name,
					State: struct {
						On  bool `json:"on"`
						Bri int  `json:"bri"`
					}{
						On:  l.On.On,
						Bri: bri,
					},
				})
			}
		}

		var nextEvent *time.Time
		if currentSim != nil {
			t := currentSim.GetNextEvent()
			nextEvent = &t
		}

		status := struct {
			Lights     []lightResponse `json:"lights"`
			NextEvent  *time.Time      `json:"next_event"`
			Configured bool            `json:"configured"`
		}{
			Lights:     lights,
			NextEvent:  nextEvent,
			Configured: currentClient != nil,
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
		sim = presence.NewSimulator(home, store)
		go sim.Run(appCtx)
	} else {
		sim.UpdateClient(home)
	}

	return nil
}
