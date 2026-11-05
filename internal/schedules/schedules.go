// Package schedules contains the presence routines the simulator can follow.
//
// A schedule is a set of windows over a day. A window activates one or more
// room categories; the simulator then picks a subset of the lamps in those
// rooms and keeps them on until the window ends. Windows are jittered per day
// so the routine does not run at exactly the same minute every day.
package schedules

import (
	"encoding/binary"
	"hash/fnv"
	"sort"
	"strings"
	"time"
)

const DefaultID = "workday"

// RoomKind is a coarse room category used to map Hue rooms to a routine.
type RoomKind string

const (
	Kitchen  RoomKind = "kitchen"
	Living   RoomKind = "living"
	Bedroom  RoomKind = "bedroom"
	Bathroom RoomKind = "bathroom"
	Office   RoomKind = "office"
	Hallway  RoomKind = "hallway"
	Outdoor  RoomKind = "outdoor"
	Other    RoomKind = "other"
)

// Window is a period during which a set of room categories is in use.
type Window struct {
	StartMinute     int // minutes since midnight
	DurationMinutes int
	Rooms           []RoomKind
}

// Definition is a selectable presence routine. Windows are not exposed over
// the API; the UI only needs the human-readable metadata.
type Definition struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Windows     []Window `json:"-"`
}

// ActiveWindow is one concrete occurrence of a window on a specific day.
type ActiveWindow struct {
	Index  int
	Window Window
	Start  time.Time
	End    time.Time
}

var definitions = []Definition{
	{
		ID:          "workday",
		Name:        "Workday",
		Description: "A normal weekday: wake up, leave for work, and return for a quiet evening.",
		Windows: []Window{
			window(6, 20, 50, Bathroom, Bedroom),
			window(6, 45, 60, Kitchen),
			window(7, 35, 35, Hallway),
			window(17, 30, 60, Hallway, Kitchen),
			window(18, 15, 75, Kitchen),
			window(18, 45, 90, Living, Other),
			window(20, 15, 165, Living, Other),
			window(22, 30, 45, Bathroom, Bedroom),
			window(23, 0, 30, Bedroom),
			window(17, 30, 360, Outdoor),
		},
	},
	{
		ID:          "home_office",
		Name:        "Home Office",
		Description: "Someone is home all day, with office lights during working hours and a relaxed evening.",
		Windows: []Window{
			window(6, 30, 50, Bathroom, Bedroom),
			window(7, 0, 60, Kitchen),
			window(8, 15, 255, Office),
			window(12, 15, 60, Kitchen),
			window(13, 0, 270, Office),
			window(17, 30, 75, Kitchen),
			window(18, 15, 105, Living, Other),
			window(20, 0, 180, Living, Other),
			window(22, 30, 45, Bathroom, Bedroom),
			window(17, 30, 360, Outdoor),
		},
	},
	{
		ID:          "weekend",
		Name:        "Weekend",
		Description: "A slow start, daytime puttering, and a long evening at home.",
		Windows: []Window{
			window(8, 15, 75, Bathroom, Bedroom),
			window(8, 45, 105, Kitchen),
			window(10, 30, 105, Living, Other),
			window(12, 0, 90, Kitchen),
			window(13, 30, 210, Living, Other),
			window(15, 0, 120, Office),
			window(17, 30, 120, Kitchen),
			window(19, 0, 120, Living, Other),
			window(21, 0, 165, Living, Other),
			window(23, 15, 60, Bathroom, Bedroom),
			window(18, 0, 360, Outdoor),
		},
	},
	{
		ID:          "night_owl",
		Name:        "Night Owl",
		Description: "A late riser with dinner and activity continuing past midnight.",
		Windows: []Window{
			window(9, 45, 60, Bathroom, Bedroom),
			window(10, 15, 75, Kitchen),
			window(12, 30, 300, Office),
			window(18, 0, 120, Kitchen),
			window(19, 30, 150, Living, Other),
			window(22, 0, 180, Living, Other),
			window(0, 30, 60, Kitchen),
			window(1, 0, 75, Bathroom, Bedroom),
			window(18, 30, 420, Outdoor),
		},
	},
	{
		ID:          "student",
		Name:        "Student",
		Description: "Late nights spent studying and a slow start to the morning.",
		Windows: []Window{
			window(8, 30, 60, Bathroom, Bedroom),
			window(9, 0, 90, Kitchen),
			window(10, 0, 480, Office),
			window(18, 30, 90, Kitchen),
			window(19, 0, 120, Living, Other),
			window(21, 0, 360, Office),
			window(23, 30, 150, Bedroom),
			window(18, 0, 360, Outdoor),
		},
	},
	{
		ID:          "shift_worker",
		Name:        "Shift Worker",
		Description: "A schedule for those working late shifts or starting late.",
		Windows: []Window{
			window(10, 0, 60, Bathroom, Bedroom),
			window(11, 0, 90, Kitchen),
			window(12, 0, 360, Office),
			window(22, 0, 90, Kitchen),
			window(23, 0, 120, Living, Other),
			window(19, 0, 360, Outdoor),
		},
	},
	{
		ID:          "pet_owner",
		Name:        "Pet Owner",
		Description: "Extra light for morning and evening activities with furry friends.",
		Windows: []Window{
			window(6, 30, 90, Kitchen, Hallway),
			window(8, 0, 480, Living, Other),
			window(17, 0, 90, Kitchen),
			window(18, 0, 120, Living, Other),
			window(20, 0, 120, Hallway, Kitchen),
			window(18, 0, 360, Outdoor),
		},
	},
	{
		ID:          "remote_hybrid",
		Name:        "Remote Hybrid",
		Description: "A blend of home office and living room presence throughout the day.",
		Windows: []Window{
			window(7, 30, 60, Bathroom, Bedroom),
			window(8, 0, 420, Office, Living),
			window(12, 0, 60, Kitchen),
			window(17, 0, 90, Kitchen),
			window(18, 0, 180, Living, Other),
			window(18, 0, 360, Outdoor),
		},
	},
	{
		ID:          "active_senior",
		Name:        "Active Senior",
		Description: "Early starts and long midday activities, with a quiet early evening.",
		Windows: []Window{
			window(6, 0, 60, Bathroom, Bedroom),
			window(6, 30, 90, Kitchen),
			window(8, 0, 480, Living, Other),
			window(16, 0, 90, Kitchen),
			window(17, 0, 120, Living, Other),
			window(20, 0, 45, Bedroom),
			window(18, 0, 360, Outdoor),
		},
	},
}

func window(startHour, startMinute, durationMinutes int, rooms ...RoomKind) Window {
	return Window{
		StartMinute:     startHour*60 + startMinute,
		DurationMinutes: durationMinutes,
		Rooms:           rooms,
	}
}

// All returns every selectable schedule in display order.
func All() []Definition {
	return definitions
}

// Default returns the schedule used when none has been chosen yet.
func Default() Definition {
	return definitions[0]
}

// Exists reports whether id identifies a known schedule.
func Exists(id string) bool {
	for _, definition := range definitions {
		if definition.ID == id {
			return true
		}
	}
	return false
}

// Normalize returns id when it is known, otherwise the default schedule ID.
func Normalize(id string) string {
	if Exists(id) {
		return id
	}
	return DefaultID
}

// Get returns the requested schedule, falling back to the default.
func Get(id string) Definition {
	id = Normalize(id)
	for _, definition := range definitions {
		if definition.ID == id {
			return definition
		}
	}
	return Default()
}

// ActiveAt returns the concrete window occurrences active at now. Both the
// current day and the previous day are checked so windows that cross midnight
// keep working after 00:00.
func (d Definition) ActiveAt(now time.Time) []ActiveWindow {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	result := make([]ActiveWindow, 0, len(d.Windows))

	for index, window := range d.Windows {
		for dayOffset := 0; dayOffset >= -1; dayOffset-- {
			start, end := window.occurrence(midnight.AddDate(0, 0, dayOffset), index)
			if !now.Before(start) && now.Before(end) {
				result = append(result, ActiveWindow{
					Index:  index,
					Window: window,
					Start:  start,
					End:    end,
				})
				break
			}
		}
	}

	return result
}

// occurrence resolves the window to concrete times, adding a deterministic
// per-day jitter so the routine varies a little from day to day.
func (w Window) occurrence(midnight time.Time, index int) (time.Time, time.Time) {
	seed := uint64(midnight.Year()*10000 + int(midnight.Month())*100 + midnight.Day())
	startJitter := int(hashUint(seed, uint64(index), 0x51ed270b)%21) - 10 // -10..+10 minutes
	durationJitter := int(hashUint(seed, uint64(index), 0x2545f491)%16) - 5
	durationMinutes := w.DurationMinutes + durationJitter
	if durationMinutes < 5 {
		durationMinutes = 5
	}

	start := midnight.Add(time.Duration(w.StartMinute+startJitter) * time.Minute)
	return start, start.Add(time.Duration(durationMinutes) * time.Minute)
}

// SelectLights deterministically chooses which lamps in a room are used for
// this window occurrence. At least one lamp is selected; rooms with multiple
// lamps get a stable subset that varies from window to window.
func (w ActiveWindow) SelectLights(room RoomKind, lightIDs []string) map[string]bool {
	selected := make(map[string]bool)
	if len(lightIDs) == 0 {
		return selected
	}

	seed := uint64(w.Start.Unix())
	count := 1 + int(hashString(seed, uint64(w.Index), string(room))%uint64(len(lightIDs)))

	type scoredLight struct {
		id    string
		score uint64
	}
	scored := make([]scoredLight, len(lightIDs))
	for i, id := range lightIDs {
		scored[i] = scoredLight{id: id, score: hashString(seed, uint64(w.Index), id)}
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score < scored[j].score
		}
		return scored[i].id < scored[j].id
	})

	for i := 0; i < count; i++ {
		selected[scored[i].id] = true
	}
	return selected
}

// ClassifyRoom maps a Hue room name to a routine category. English and German
// names are recognised, which covers the common Hue app naming schemes.
func ClassifyRoom(name string) RoomKind {
	lower := strings.ToLower(strings.TrimSpace(name))

	switch {
	case containsAny(lower, "carport", "schuppen", "garten", "terrasse", "outdoor", "außen", "aussen", "balkon", "garage", "veranda", "patio", "shed", "porch"):
		return Outdoor
	case containsAny(lower, "küche", "kueche", "kitchen", "esszimmer", "dining", "cocina", "cuisine"):
		return Kitchen
	case containsAny(lower, "bad", "bath", "wc", "toilet", "dusche", "shower"):
		return Bathroom
	case containsAny(lower, "schlafzimmer", "bedroom", "kinderzimmer", "gästezimmer", "guest", "nursery", "kids", "child"):
		return Bedroom
	case containsAny(lower, "arbeitszimmer", "office", "büro", "buero", "study", "atelier", "werkstatt", "desk", "work"):
		return Office
	case containsAny(lower, "wohnzimmer", "living", "lounge", "salon", "stube", "lesezimmer", "reading", "bibliothek", "library", "familie", "tv"):
		return Living
	case containsAny(lower, "flur", "hall", "diele", "entry", "corridor", "treppe", "stairs", "vorraum"):
		return Hallway
	default:
		return Other
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func hashUint(seed uint64, values ...uint64) uint64 {
	hash := fnv.New64a()
	var buffer [8]byte
	binary.LittleEndian.PutUint64(buffer[:], seed)
	_, _ = hash.Write(buffer[:])
	for _, value := range values {
		binary.LittleEndian.PutUint64(buffer[:], value)
		_, _ = hash.Write(buffer[:])
	}
	return hash.Sum64()
}

func hashString(seed, index uint64, value string) uint64 {
	hash := fnv.New64a()
	var buffer [8]byte
	binary.LittleEndian.PutUint64(buffer[:], seed)
	_, _ = hash.Write(buffer[:])
	binary.LittleEndian.PutUint64(buffer[:], index)
	_, _ = hash.Write(buffer[:])
	_, _ = hash.Write([]byte(value))
	return hash.Sum64()
}
