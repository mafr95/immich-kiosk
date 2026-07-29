package calendar

import (
	"strings"
	"testing"
	"time"

	ics "github.com/arran4/golang-ical"
)

func mustParseCalendar(t *testing.T, raw string) *ics.Calendar {
	t.Helper()
	cal, err := ics.ParseCalendar(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("failed to parse test calendar: %v", err)
	}
	return cal
}

func icsDoc(events string) string {
	return "BEGIN:VCALENDAR\n" +
		"VERSION:2.0\n" +
		"PRODID:-//test//test\n" +
		events +
		"END:VCALENDAR\n"
}

var (
	windowStart = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	windowEnd   = time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
)

func TestExpandEvents_NonRecurringWithinWindow(t *testing.T) {
	raw := icsDoc(
		"BEGIN:VEVENT\n" +
			"UID:1@test\n" +
			"DTSTAMP:20240101T000000Z\n" +
			"DTSTART:20240115T100000Z\n" +
			"DTEND:20240115T110000Z\n" +
			"SUMMARY:Team Sync\n" +
			"END:VEVENT\n",
	)
	cal := mustParseCalendar(t, raw)

	events := expandEvents(cal, "work", "#fff", windowStart, windowEnd)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Summary != "Team Sync" {
		t.Errorf("expected summary 'Team Sync', got %q", events[0].Summary)
	}
	if events[0].AllDay {
		t.Errorf("expected timed event, got all-day")
	}
	want := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	if !events[0].Start.Equal(want) {
		t.Errorf("expected start %v, got %v", want, events[0].Start)
	}
}

func TestExpandEvents_NonRecurringOutsideWindow(t *testing.T) {
	raw := icsDoc(
		"BEGIN:VEVENT\n" +
			"UID:2@test\n" +
			"DTSTAMP:20240101T000000Z\n" +
			"DTSTART:20240601T100000Z\n" +
			"DTEND:20240601T110000Z\n" +
			"SUMMARY:Summer Party\n" +
			"END:VEVENT\n",
	)
	cal := mustParseCalendar(t, raw)

	events := expandEvents(cal, "work", "#fff", windowStart, windowEnd)

	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
}

func TestExpandEvents_WeeklyRecurrenceWithCount(t *testing.T) {
	raw := icsDoc(
		"BEGIN:VEVENT\n" +
			"UID:3@test\n" +
			"DTSTAMP:20240101T000000Z\n" +
			"DTSTART:20240102T090000Z\n" +
			"DTEND:20240102T093000Z\n" +
			"SUMMARY:Standup\n" +
			"RRULE:FREQ=WEEKLY;COUNT=3\n" +
			"END:VEVENT\n",
	)
	cal := mustParseCalendar(t, raw)

	events := expandEvents(cal, "work", "#fff", windowStart, windowEnd)

	if len(events) != 3 {
		t.Fatalf("expected 3 occurrences, got %d", len(events))
	}
	wantDates := []string{"2024-01-02", "2024-01-09", "2024-01-16"}
	for i, want := range wantDates {
		got := events[i].Start.UTC().Format("2006-01-02")
		if got != want {
			t.Errorf("occurrence %d: expected date %s, got %s", i, want, got)
		}
		wantEnd := events[i].Start.Add(30 * time.Minute)
		if !events[i].End.Equal(wantEnd) {
			t.Errorf("occurrence %d: expected end %v, got %v", i, wantEnd, events[i].End)
		}
	}
}

func TestExpandEvents_RecurrenceBoundedByWindowNotCount(t *testing.T) {
	// COUNT=6 monthly occurrences would run through June, but the window
	// only extends to March 1st, so only Jan/Feb occurrences should appear.
	raw := icsDoc(
		"BEGIN:VEVENT\n" +
			"UID:4@test\n" +
			"DTSTAMP:20240101T000000Z\n" +
			"DTSTART:20240105T080000Z\n" +
			"DTEND:20240105T090000Z\n" +
			"SUMMARY:Monthly Review\n" +
			"RRULE:FREQ=MONTHLY;COUNT=6\n" +
			"END:VEVENT\n",
	)
	cal := mustParseCalendar(t, raw)

	events := expandEvents(cal, "work", "#fff", windowStart, windowEnd)

	if len(events) != 2 {
		t.Fatalf("expected 2 occurrences bounded by window, got %d", len(events))
	}
	if got := events[len(events)-1].Start.UTC().Format("2006-01-02"); got != "2024-02-05" {
		t.Errorf("expected last occurrence 2024-02-05, got %s", got)
	}
}

func TestExpandEvents_ExdateExcludesOccurrence(t *testing.T) {
	raw := icsDoc(
		"BEGIN:VEVENT\n" +
			"UID:5@test\n" +
			"DTSTAMP:20240101T000000Z\n" +
			"DTSTART:20240103T090000Z\n" +
			"DTEND:20240103T093000Z\n" +
			"SUMMARY:Weekly Sync\n" +
			"RRULE:FREQ=WEEKLY;COUNT=4\n" +
			"EXDATE:20240110T090000Z\n" +
			"END:VEVENT\n",
	)
	cal := mustParseCalendar(t, raw)

	events := expandEvents(cal, "work", "#fff", windowStart, windowEnd)

	if len(events) != 3 {
		t.Fatalf("expected 3 occurrences after exclusion, got %d", len(events))
	}
	for _, e := range events {
		if e.Start.UTC().Format("2006-01-02") == "2024-01-10" {
			t.Errorf("excluded date 2024-01-10 should not be present")
		}
	}
}

func TestExpandEvents_AllDayEvent(t *testing.T) {
	raw := icsDoc(
		"BEGIN:VEVENT\n" +
			"UID:6@test\n" +
			"DTSTAMP:20240101T000000Z\n" +
			"DTSTART;VALUE=DATE:20240120\n" +
			"DTEND;VALUE=DATE:20240121\n" +
			"SUMMARY:Birthday\n" +
			"END:VEVENT\n",
	)
	cal := mustParseCalendar(t, raw)

	events := expandEvents(cal, "personal", "#fff", windowStart, windowEnd)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if !events[0].AllDay {
		t.Errorf("expected all-day event")
	}
	if got := events[0].Start.Format("2006-01-02"); got != "2024-01-20" {
		t.Errorf("expected start date 2024-01-20, got %s", got)
	}
}

func TestCurrentEvents_FiltersToRelevantWindow(t *testing.T) {
	now := time.Now()

	ended := Event{CalendarName: "a", Summary: "Already ended", Start: now.Add(-2 * time.Hour), End: now.Add(-time.Hour)}
	inProgress := Event{CalendarName: "a", Summary: "In progress", Start: now.Add(-10 * time.Minute), End: now.Add(20 * time.Minute)}
	withinLeadTime := Event{CalendarName: "a", Summary: "Starting in 20 min", Start: now.Add(20 * time.Minute), End: now.Add(50 * time.Minute)}
	tooFarAhead := Event{CalendarName: "b", Summary: "Starting in 45 min", Start: now.Add(45 * time.Minute), End: now.Add(75 * time.Minute)}
	allDayToday := Event{CalendarName: "b", Summary: "Birthday", AllDay: true, Start: now.Add(-6 * time.Hour), End: now.Add(6 * time.Hour)}

	calendarDataStore.Store("a", []Event{ended, inProgress, withinLeadTime})
	calendarDataStore.Store("b", []Event{tooFarAhead, allDayToday})
	t.Cleanup(func() {
		calendarDataStore.Delete("a")
		calendarDataStore.Delete("b")
	})

	got := CurrentEvents(0)

	if len(got) != 3 {
		t.Fatalf("expected 3 currently relevant events, got %d: %+v", len(got), got)
	}
	wantOrder := []string{"Birthday", "In progress", "Starting in 20 min"}
	for i, want := range wantOrder {
		if got[i].Summary != want {
			t.Errorf("event %d: expected %q, got %q", i, want, got[i].Summary)
		}
	}
}

func TestCurrentEvents_CapsAtMaxEvents(t *testing.T) {
	now := time.Now()

	first := Event{CalendarName: "a", Summary: "First", Start: now.Add(-time.Minute), End: now.Add(time.Hour)}
	second := Event{CalendarName: "a", Summary: "Second", Start: now, End: now.Add(time.Hour)}

	calendarDataStore.Store("a", []Event{first, second})
	t.Cleanup(func() {
		calendarDataStore.Delete("a")
	})

	got := CurrentEvents(1)

	if len(got) != 1 {
		t.Fatalf("expected 1 event (capped), got %d", len(got))
	}
	if got[0].Summary != "First" {
		t.Errorf("expected 'First' (earliest start), got %q", got[0].Summary)
	}
}
