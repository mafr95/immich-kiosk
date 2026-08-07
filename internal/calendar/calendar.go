package calendar

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	// Ensure IANA timezone data is available even on minimal runtime images
	// (e.g. distroless/scratch containers) that don't ship OS tzdata.
	_ "time/tzdata"

	"charm.land/log/v2"
	ics "github.com/arran4/golang-ical"
	"github.com/damongolding/immich-kiosk/internal/config"
	rrule "github.com/teambition/rrule-go"
)

// calendarDataStore holds the most recently fetched, expanded events for each
// configured calendar, keyed by the lowercased calendar name.
var calendarDataStore sync.Map

var (
	httpTransport = &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
		MaxIdleConnsPerHost: 100,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	httpClient = &http.Client{
		Transport: httpTransport,
		Timeout:   30 * time.Second,
	}
)

var freqMap = map[ics.Frequency]rrule.Frequency{
	ics.FrequencySecondly: rrule.SECONDLY,
	ics.FrequencyMinutely: rrule.MINUTELY,
	ics.FrequencyHourly:   rrule.HOURLY,
	ics.FrequencyDaily:    rrule.DAILY,
	ics.FrequencyWeekly:   rrule.WEEKLY,
	ics.FrequencyMonthly:  rrule.MONTHLY,
	ics.FrequencyYearly:   rrule.YEARLY,
}

var weekdayMap = map[ics.Weekday]rrule.Weekday{
	ics.WeekdaySunday:    rrule.SU,
	ics.WeekdayMonday:    rrule.MO,
	ics.WeekdayTuesday:   rrule.TU,
	ics.WeekdayWednesday: rrule.WE,
	ics.WeekdayThursday:  rrule.TH,
	ics.WeekdayFriday:    rrule.FR,
	ics.WeekdaySaturday:  rrule.SA,
}

// Event represents a single calendar event occurrence (recurring events are
// expanded into one Event per occurrence within the configured lookahead window).
type Event struct {
	CalendarName string
	Color        string
	Summary      string
	Location     string
	AllDay       bool
	Start        time.Time
	End          time.Time
}

// AddCalendar fetches a calendar feed, stores its upcoming events, and then
// refreshes them on a ticker until ctx is cancelled.
func AddCalendar(ctx context.Context, cal config.Calendar, lookahead, refreshInterval time.Duration) {
	log.Info("Starting calendar sync", "name", cal.Name, "refresh_interval", refreshInterval, "lookahead", lookahead)

	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	syncCalendar(ctx, cal, lookahead)

	for {
		select {
		case <-ctx.Done():
			log.Debug("Stopping calendar sync", "name", cal.Name)
			return
		case <-ticker.C:
			syncCalendar(ctx, cal, lookahead)
		}
	}
}

// syncCalendar refreshes a single calendar and logs the outcome.
func syncCalendar(ctx context.Context, cal config.Calendar, lookahead time.Duration) {
	log.Debug("Syncing calendar", "name", cal.Name)

	start := time.Now()
	events, err := refreshCalendar(ctx, cal, lookahead)
	if err != nil {
		log.Error("Failed to sync calendar", "name", cal.Name, "error", err)
		return
	}

	log.Info("Synced calendar", "name", cal.Name, "events", len(events), "duration", time.Since(start))
}

// refreshCalendar fetches and parses cal's ICS feed and stores its expanded
// events (bounded to a day in the past through now+lookahead) in calendarDataStore.
func refreshCalendar(ctx context.Context, cal config.Calendar, lookahead time.Duration) ([]Event, error) {
	parsed, err := fetchICS(ctx, cal.Name, cal.URL)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	events := expandEvents(parsed, cal.Name, cal.Color, now.Add(-24*time.Hour), now.Add(lookahead))

	calendarDataStore.Store(strings.ToLower(cal.Name), events)

	return events, nil
}

// fetchICS fetches and parses an .ics feed from the given URL, retrying up to
// 3 times with exponential backoff on transport errors. calName is only used
// for log context - the URL itself is never logged since it typically
// contains a secret token.
func fetchICS(ctx context.Context, calName, url string) (*ics.Calendar, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "text/calendar")

	var res *http.Response
	for attempt := range 3 {
		res, err = httpClient.Do(req)
		if err == nil {
			break
		}
		log.Warn("Calendar request failed, retrying", "name", calName, "attempt", attempt+1, "error", err)

		backoff := time.Duration(1<<attempt) * time.Second
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}

	if err != nil {
		log.Error("Calendar request failed after retries", "name", calName, "error", err)
		return nil, err
	}
	defer res.Body.Close()

	log.Debug("Fetched calendar feed", "name", calName, "status", res.StatusCode)

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	parsed, err := ics.ParseCalendar(res.Body)
	if err != nil {
		log.Error("Failed to parse calendar feed", "name", calName, "error", err)
		return nil, err
	}

	return parsed, nil
}

// expandEvents walks every VEVENT in parsed, expanding any RRULE recurrences
// bounded to [windowStart, windowEnd], and returns the resulting occurrences
// sorted by start time. This bounding keeps expansion cheap for rules with a
// far-future or absent UNTIL.
func expandEvents(parsed *ics.Calendar, calName, color string, windowStart, windowEnd time.Time) []Event {
	var events []Event

	for _, vevent := range parsed.Events() {
		start, err := vevent.GetStartAt()
		if err != nil {
			log.Warn("Skipping event with unparsable DTSTART", "calendar", calName, "uid", propertyValue(vevent, ics.ComponentPropertyUniqueId), "error", err)
			continue
		}

		allDay := isAllDay(vevent)

		end, endErr := vevent.GetEndAt()
		if endErr != nil {
			if allDay {
				end = start.Add(24 * time.Hour)
			} else {
				end = start.Add(time.Hour)
			}
		}
		duration := end.Sub(start)
		widenBy := duration
		if widenBy < 0 {
			widenBy = 0
		}

		summary := propertyValue(vevent, ics.ComponentPropertySummary)
		location := propertyValue(vevent, ics.ComponentPropertyLocation)

		rrules, rruleErr := vevent.GetRRules()
		if rruleErr != nil || len(rrules) == 0 {
			// Include if the event's [start, end) span overlaps the window at
			// all, not just if it starts within it - otherwise multi-day
			// events that started before the window (e.g. yesterday) but are
			// still ongoing would be missed entirely.
			if !end.Before(windowStart) && !start.After(windowEnd) {
				events = append(events, Event{
					CalendarName: calName,
					Color:        color,
					Summary:      summary,
					Location:     location,
					AllDay:       allDay,
					Start:        start,
					End:          end,
				})
			}
			continue
		}

		set := &rrule.Set{}

		// RFC 5545 permits only one RRULE per component.
		rr, rrErr := toRRule(rrules[0], start)
		if rrErr != nil {
			log.Warn("Skipping unparsable RRULE", "calendar", calName, "error", rrErr)
			continue
		}
		set.RRule(rr)

		if exdates, exErr := vevent.GetExDates(); exErr == nil {
			for _, ex := range exdates {
				set.ExDate(ex)
			}
		}
		if rdates, rdErr := vevent.GetRDates(); rdErr == nil {
			for _, rd := range rdates {
				set.RDate(rd)
			}
		}

		// Search from windowStart-duration so multi-day (or multi-hour)
		// occurrences that started before the window but are still ongoing
		// when it begins aren't missed by rrule's start-time-only matching.
		for _, occStart := range set.Between(windowStart.Add(-widenBy), windowEnd, true) {
			occEnd := occStart.Add(duration)
			if occEnd.Before(windowStart) {
				continue
			}
			events = append(events, Event{
				CalendarName: calName,
				Color:        color,
				Summary:      summary,
				Location:     location,
				AllDay:       allDay,
				Start:        occStart,
				End:          occEnd,
			})
		}
	}

	sort.Slice(events, func(i, j int) bool { return events[i].Start.Before(events[j].Start) })

	return events
}

// toRRule converts a parsed ICS RecurrenceRule into an rrule-go RRule anchored at dtstart.
func toRRule(r *ics.RecurrenceRule, dtstart time.Time) (*rrule.RRule, error) {
	freq, ok := freqMap[r.Freq]
	if !ok {
		return nil, fmt.Errorf("unsupported RRULE frequency: %s", r.Freq)
	}

	opt := rrule.ROption{
		Freq:       freq,
		Dtstart:    dtstart,
		Interval:   r.Interval,
		Count:      r.Count,
		Until:      r.Until,
		Bymonth:    r.ByMonth,
		Bymonthday: r.ByMonthDay,
		Byyearday:  r.ByYearDay,
		Byweekno:   r.ByWeekNo,
		Bysetpos:   r.BySetPos,
	}

	for _, wd := range r.ByDay {
		weekday, ok := weekdayMap[wd.Day]
		if !ok {
			continue
		}
		opt.Byweekday = append(opt.Byweekday, weekday.Nth(wd.OrdWeek))
	}

	return rrule.NewRRule(opt)
}

// isAllDay reports whether vevent's DTSTART is a date-only value (VALUE=DATE),
// as used by Google Calendar for all-day events.
func isAllDay(vevent *ics.VEvent) bool {
	prop := vevent.GetProperty(ics.ComponentPropertyDtStart)
	if prop == nil {
		return false
	}

	for _, v := range prop.ICalParameters["VALUE"] {
		if strings.EqualFold(v, "DATE") {
			return true
		}
	}

	return false
}

func propertyValue(vevent *ics.VEvent, prop ics.ComponentProperty) string {
	p := vevent.GetProperty(prop)
	if p == nil {
		return ""
	}
	return p.Value
}

// CurrentEvents returns the merged, sorted list of currently relevant events
// across all configured calendars, capped at maxEvents (0 or negative means
// unlimited). An all-day event is relevant for its entire day; a timed event
// becomes relevant from the start of its calendar day and stops being
// relevant once it ends.
func CurrentEvents(maxEvents int) []Event {
	now := time.Now()

	var all []Event
	calendarDataStore.Range(func(_, value any) bool {
		events, ok := value.([]Event)
		if !ok {
			return true
		}
		for _, e := range events {
			visibleFrom := e.Start
			if !e.AllDay {
				visibleFrom = startOfDay(e.Start)
			}
			if now.Before(visibleFrom) || !now.Before(e.End) {
				continue
			}
			all = append(all, e)
		}
		return true
	})

	sort.Slice(all, func(i, j int) bool { return all[i].Start.Before(all[j].Start) })

	if maxEvents > 0 && len(all) > maxEvents {
		all = all[:maxEvents]
	}

	return all
}

// startOfDay returns midnight of t's calendar day, in t's location.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
