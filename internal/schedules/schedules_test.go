package schedules

import (
	"testing"
	"time"
)

// TestOccurrencesOnDayCoversTheWholeDay checks the data behind the UI
// timeline: every occurrence must fall inside the day it is drawn on, and
// windows reaching across midnight have to show up on both days.
func TestOccurrencesOnDayCoversTheWholeDay(t *testing.T) {
	day := time.Date(2024, 3, 14, 13, 0, 0, 0, time.Local)
	dayStart := startOfDay(day)
	dayEnd := dayStart.AddDate(0, 0, 1)

	for _, definition := range All() {
		t.Run(definition.ID, func(t *testing.T) {
			occurrences := definition.OccurrencesOnDay(day)
			if len(occurrences) < len(definition.Windows) {
				t.Fatalf("got %d occurrences for %d windows, want at least one each",
					len(occurrences), len(definition.Windows))
			}

			var previous time.Time
			for _, occurrence := range occurrences {
				if !occurrence.End.After(dayStart) || !occurrence.Start.Before(dayEnd) {
					t.Fatalf("occurrence %v..%v does not overlap the day %v..%v",
						occurrence.Start, occurrence.End, dayStart, dayEnd)
				}
				if !occurrence.End.After(occurrence.Start) {
					t.Fatalf("occurrence %v..%v is empty", occurrence.Start, occurrence.End)
				}
				if occurrence.Start.Before(previous) {
					t.Fatalf("occurrences are not sorted: %v after %v", occurrence.Start, previous)
				}
				previous = occurrence.Start
			}
		})
	}
}

// TestOccurrencesOnDayIncludesWindowsFromNeighbouringDays covers the Night
// Owl routine, whose late windows run past midnight into the next day.
func TestOccurrencesOnDayIncludesWindowsFromNeighbouringDays(t *testing.T) {
	day := time.Date(2024, 3, 14, 13, 0, 0, 0, time.Local)
	dayStart := startOfDay(day)

	occurrences := Get("night_owl").OccurrencesOnDay(day)

	var fromPreviousDay bool
	for _, occurrence := range occurrences {
		if occurrence.Start.Before(dayStart) {
			fromPreviousDay = true
		}
	}
	if !fromPreviousDay {
		t.Fatal("expected a window started the previous evening to reach into this day")
	}
}

// TestActiveAtAgreesWithOccurrences guards the two code paths that answer
// "what is running now": the simulator uses ActiveAt, the UI timeline marks
// occurrences active. They must not disagree.
func TestActiveAtAgreesWithOccurrences(t *testing.T) {
	definition := Get("workday")

	for hour := 0; hour < 24; hour++ {
		now := time.Date(2024, 3, 14, hour, 30, 0, 0, time.Local)

		active := map[int]bool{}
		for _, window := range definition.ActiveAt(now) {
			active[window.Index] = true
		}

		marked := map[int]bool{}
		for _, occurrence := range definition.OccurrencesOnDay(now) {
			if occurrence.Contains(now) {
				marked[occurrence.Index] = true
			}
		}

		if len(active) != len(marked) {
			t.Fatalf("at %v: ActiveAt reported %v, the timeline marked %v", now, active, marked)
		}
		for index := range active {
			if !marked[index] {
				t.Fatalf("at %v: window %d is active but not marked on the timeline", now, index)
			}
		}
	}
}

// TestOccurrenceKeepsWallClockStartAcrossDST pins the behaviour that makes a
// routine follow the household's own clock. Computing a start by adding
// elapsed minutes to midnight drags every window an hour off on the two days a
// year the calendar day is not 24 hours long, so a 06:20 wake-up fired at
// 07:20 in spring and 05:20 in autumn.
func TestOccurrenceKeepsWallClockStartAcrossDST(t *testing.T) {
	// Europe/Berlin rather than time.Local: the test has to assert the same
	// thing whatever zone the machine running it is in.
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("Europe/Berlin zone data unavailable: %v", err)
	}

	days := map[string]time.Time{
		"regular":        time.Date(2024, 3, 30, 0, 0, 0, 0, berlin),
		"spring_forward": time.Date(2024, 3, 31, 0, 0, 0, 0, berlin),  // 02:00 -> 03:00
		"fall_back":      time.Date(2024, 10, 27, 0, 0, 0, 0, berlin), // 03:00 -> 02:00
	}

	// The widest jitter occurrence() applies to a start time.
	const maxJitterMinutes = 10

	for name, midnight := range days {
		t.Run(name, func(t *testing.T) {
			for _, definition := range All() {
				for index, window := range definition.Windows {
					start, _ := window.occurrence(midnight, index)

					drift := minutesBetweenOnClock(window.StartMinute, start)
					if drift > maxJitterMinutes {
						t.Errorf("%s window %d: start %s is %d wall-clock minutes from %02d:%02d, want at most %d",
							definition.ID, index, start.Format("15:04 MST"), drift,
							window.StartMinute/60, window.StartMinute%60, maxJitterMinutes)
					}
				}
			}
		})
	}
}

// minutesBetweenOnClock returns how far the wall-clock time of actual is from
// the given minute of the day, measured the short way around the clock so a
// window jittered back across midnight is not reported as a 24-hour drift.
func minutesBetweenOnClock(wantMinute int, actual time.Time) int {
	actualMinute := actual.Hour()*60 + actual.Minute()

	difference := actualMinute - wantMinute
	difference = ((difference % 1440) + 1440) % 1440
	if difference > 720 {
		difference = 1440 - difference
	}
	return difference
}

func TestDesiredStatesOnlyLightsActiveRooms(t *testing.T) {
	roomOfLight := map[string]RoomKind{
		"kitchen-1": Kitchen,
		"kitchen-2": Kitchen,
		"bedroom-1": Bedroom,
	}
	lightIDs := []string{"kitchen-1", "kitchen-2", "bedroom-1", "unclassified-1"}

	start := time.Date(2024, 3, 14, 18, 0, 0, 0, time.Local)
	active := []ActiveWindow{{
		Index:  0,
		Window: Window{Rooms: []RoomKind{Kitchen}},
		Start:  start,
		End:    start.Add(time.Hour),
	}}

	desired := DesiredStates(active, roomOfLight, lightIDs)

	if len(desired) == 0 {
		t.Fatal("expected at least one kitchen lamp to be wanted on")
	}
	for id := range desired {
		if roomOfLight[id] != Kitchen {
			t.Fatalf("lamp %q is not in the kitchen but was wanted on", id)
		}
	}
}

// TestDesiredStatesIgnoresLampOrder matters because the simulator and the
// status API build their candidate list from different places; a different
// order must not make them pick different lamps.
func TestDesiredStatesIgnoresLampOrder(t *testing.T) {
	roomOfLight := map[string]RoomKind{"a": Living, "b": Living, "c": Living}
	start := time.Date(2024, 3, 14, 20, 0, 0, 0, time.Local)
	active := []ActiveWindow{{
		Index:  3,
		Window: Window{Rooms: []RoomKind{Living}},
		Start:  start,
		End:    start.Add(2 * time.Hour),
	}}

	forward := DesiredStates(active, roomOfLight, []string{"a", "b", "c"})
	reversed := DesiredStates(active, roomOfLight, []string{"c", "b", "a"})

	if len(forward) != len(reversed) {
		t.Fatalf("selected %d lamps in one order and %d in the other", len(forward), len(reversed))
	}
	for id := range forward {
		if !reversed[id] {
			t.Fatalf("lamp %q was selected in one order but not the other", id)
		}
	}
}

func TestClassifyRoom(t *testing.T) {
	tests := map[string]RoomKind{
		"Küche":         Kitchen,
		"Kitchen":       Kitchen,
		"Wohnzimmer":    Living,
		"Carport":       Outdoor,
		"Arbeitszimmer": Office,
		"Badezimmer":    Bathroom,
		"Flur":          Hallway,
		"Schlafzimmer":  Bedroom,
		"Abstellkammer": Other,
	}

	for name, want := range tests {
		if got := ClassifyRoom(name); got != want {
			t.Errorf("ClassifyRoom(%q) = %q, want %q", name, got, want)
		}
	}
}
