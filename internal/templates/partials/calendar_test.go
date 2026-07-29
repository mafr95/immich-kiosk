package partials

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/damongolding/immich-kiosk/internal/calendar"
	"github.com/goodsign/monday"
)

// TestMain forces a fixed, non-UTC local timezone for this package's tests
// so that calendarEventWhen's conversion of event times to time.Local is
// actually exercised, regardless of the machine/CI runner's own timezone.
func TestMain(m *testing.M) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		panic(err)
	}
	time.Local = loc
	os.Exit(m.Run())
}

func fakeTranslate(key string) string {
	return "[" + key + "]"
}

func TestCalendarEventWhen_AllDaySingleDay(t *testing.T) {
	event := calendar.Event{
		AllDay: true,
		Start:  time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC),
		End:    time.Date(2024, 1, 21, 0, 0, 0, 0, time.UTC),
	}

	got := calendarEventWhen(event, monday.LocaleEnUS, fakeTranslate)

	want := fakeTranslate("all_day")
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestCalendarEventWhen_AllDayMultiDay(t *testing.T) {
	event := calendar.Event{
		AllDay: true,
		Start:  time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC),
		End:    time.Date(2024, 1, 23, 0, 0, 0, 0, time.UTC), // exclusive: last day is Jan 22
	}

	got := calendarEventWhen(event, monday.LocaleEnUS, fakeTranslate)

	displayEnd := event.End.Local().AddDate(0, 0, -1)
	want := fmt.Sprintf("%s %s %s", fakeTranslate("all_day"), fakeTranslate("until"), monday.Format(displayEnd, "Mon, Jan 2", monday.LocaleEnUS))
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestCalendarEventWhen_ConvertsToLocalTimezone reproduces a real report: an
// event stored as 00:00-01:00 in the UTC+2 (CEST) source calendar arrives as
// 22:00-23:00Z in UTC. Without converting to time.Local before formatting,
// the widget displayed the raw UTC hour (22:00) instead of the correct local
// time (00:00).
func TestCalendarEventWhen_ConvertsToLocalTimezone(t *testing.T) {
	event := calendar.Event{
		Start: time.Date(2024, 6, 19, 22, 0, 0, 0, time.UTC), // 2024-06-20 00:00 CEST
		End:   time.Date(2024, 6, 19, 23, 0, 0, 0, time.UTC), // 2024-06-20 01:00 CEST
	}

	got := calendarEventWhen(event, monday.LocaleEnUS, fakeTranslate)

	want := fmt.Sprintf("%s–%s", monday.Format(event.Start.Local(), "Mon, Jan 2 15:04", monday.LocaleEnUS), monday.Format(event.End.Local(), "15:04", monday.LocaleEnUS))
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
	if got == fmt.Sprintf("%s–%s", monday.Format(event.Start, "Mon, Jan 2 15:04", monday.LocaleEnUS), monday.Format(event.End, "15:04", monday.LocaleEnUS)) {
		t.Errorf("output still uses raw UTC time instead of local time: %q", got)
	}
}

func TestCalendarEventWhen_TimedSingleDay(t *testing.T) {
	event := calendar.Event{
		Start: time.Date(2024, 1, 20, 14, 0, 0, 0, time.UTC),
		End:   time.Date(2024, 1, 20, 15, 0, 0, 0, time.UTC),
	}

	got := calendarEventWhen(event, monday.LocaleEnUS, fakeTranslate)

	want := fmt.Sprintf("%s–%s", monday.Format(event.Start.Local(), "Mon, Jan 2 15:04", monday.LocaleEnUS), monday.Format(event.End.Local(), "15:04", monday.LocaleEnUS))
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestCalendarEventWhen_TimedMultiDay(t *testing.T) {
	event := calendar.Event{
		Start: time.Date(2024, 1, 20, 14, 0, 0, 0, time.UTC),
		End:   time.Date(2024, 1, 22, 16, 0, 0, 0, time.UTC),
	}

	got := calendarEventWhen(event, monday.LocaleEnUS, fakeTranslate)

	want := fmt.Sprintf("%s %s %s",
		monday.Format(event.Start.Local(), "Mon, Jan 2 15:04", monday.LocaleEnUS),
		fakeTranslate("until"),
		monday.Format(event.End.Local(), "Mon, Jan 2 15:04", monday.LocaleEnUS),
	)
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestGroupEventsByCalendar(t *testing.T) {
	events := []calendar.Event{
		{CalendarName: "Work", Summary: "Standup", Start: time.Date(2024, 1, 20, 9, 0, 0, 0, time.UTC)},
		{CalendarName: "Personal", Summary: "Birthday", AllDay: true, Start: time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC)},
		{CalendarName: "Work", Summary: "Review", Start: time.Date(2024, 1, 20, 15, 0, 0, 0, time.UTC)},
	}

	groups := groupEventsByCalendar(events)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].CalendarName != "Personal" || groups[1].CalendarName != "Work" {
		t.Fatalf("expected groups sorted alphabetically (Personal, Work), got (%s, %s)", groups[0].CalendarName, groups[1].CalendarName)
	}
	if len(groups[1].Events) != 2 {
		t.Fatalf("expected 2 events in 'Work' group, got %d", len(groups[1].Events))
	}
	if groups[1].Events[0].Summary != "Standup" || groups[1].Events[1].Summary != "Review" {
		t.Errorf("expected 'Work' group to preserve input order (Standup, Review), got (%s, %s)", groups[1].Events[0].Summary, groups[1].Events[1].Summary)
	}
}

func TestGroupEventsByCalendar_Empty(t *testing.T) {
	groups := groupEventsByCalendar(nil)
	if len(groups) != 0 {
		t.Errorf("expected no groups for empty input, got %d", len(groups))
	}
}
