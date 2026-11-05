package presence

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openhue/openhue-go"
	"lampengeist/internal/config"
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
	if got, want := scaledDelay(simulated, config.MaxSimulationSpeed), 100*time.Millisecond; got < want {
		t.Fatalf("max speed delay = %v, want at least %v", got, want)
	}
	if got, want := scaledDelay(simulated, 0), simulated; got != want {
		t.Fatalf("invalid speed delay = %v, want %v", got, want)
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
