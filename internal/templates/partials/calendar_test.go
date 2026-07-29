package partials

import (
	"fmt"
	"testing"
	"time"

	"github.com/damongolding/immich-kiosk/internal/calendar"
	"github.com/goodsign/monday"
)

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

	want := fmt.Sprintf("%s %s %s", fakeTranslate("all_day"), fakeTranslate("until"), monday.Format(time.Date(2024, 1, 22, 0, 0, 0, 0, time.UTC), "Mon, Jan 2", monday.LocaleEnUS))
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestCalendarEventWhen_TimedSingleDay(t *testing.T) {
	event := calendar.Event{
		Start: time.Date(2024, 1, 20, 14, 0, 0, 0, time.UTC),
		End:   time.Date(2024, 1, 20, 15, 0, 0, 0, time.UTC),
	}

	got := calendarEventWhen(event, monday.LocaleEnUS, fakeTranslate)

	want := monday.Format(event.Start, "Mon, Jan 2 15:04", monday.LocaleEnUS)
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
		monday.Format(event.Start, "Mon, Jan 2 15:04", monday.LocaleEnUS),
		fakeTranslate("until"),
		monday.Format(event.End, "Mon, Jan 2 15:04", monday.LocaleEnUS),
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
