package caldav

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/emersion/go-ical"
)

// Bound each duration component before the library multiplies its counts. The
// accepted subset cannot overflow time.Duration; the total is bounded below too.
var periodDuration = regexp.MustCompile(`^P(?:[0-9]{1,3}W|(?:[0-9]{1,3}D)?(?:T(?:[0-9]{1,4}H)?(?:[0-9]{1,6}M)?(?:[0-9]{1,8}S)?)?)$`)

const (
	maxReportBytes    = 4 << 20
	maxBusyPeriods    = 4096
	maxCalendars      = 8
	maxCoverage       = 31 * 24 * time.Hour
	calendarUTCLayout = "20060102T150405Z"
)

type freeBusyQuery struct {
	XMLName xml.Name    `xml:"urn:ietf:params:xml:ns:caldav free-busy-query"`
	Range   reportRange `xml:"urn:ietf:params:xml:ns:caldav time-range"`
}

type reportRange struct {
	Start string `xml:"start,attr"`
	End   string `xml:"end,attr"`
}

// Busy returns no partial calendar set and never exposes resource identifiers or
// provider diagnostics. Recurrence and timezone expansion belong to the server.
func (c *Client) Busy(ctx context.Context, coverage booking.Interval) (booking.BusySnapshot, error) {
	if !c.validSources() || coverage.Start.IsZero() || !coverage.Start.Before(coverage.End) || coverage.End.Sub(coverage.Start) > maxCoverage {
		return booking.BusySnapshot{}, booking.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	observed := c.now().UTC()
	if observed.IsZero() || ctx.Err() != nil {
		return booking.BusySnapshot{}, booking.ErrUnavailable
	}
	data, err := xml.Marshal(freeBusyQuery{Range: reportRange{Start: coverage.Start.UTC().Format(calendarUTCLayout), End: coverage.End.UTC().Format(calendarUTCLayout)}})
	if err != nil {
		return booking.BusySnapshot{}, booking.ErrUnavailable
	}
	client, cleanup, err := c.sourceClient(ctx)
	if err != nil {
		return booking.BusySnapshot{}, booking.ErrUnavailable
	}
	defer cleanup()
	busy := make([]booking.Interval, 0)
	for _, collection := range c.calendars {
		periods, err := c.report(ctx, client, collection, data, coverage)
		if err != nil || len(periods) > maxBusyPeriods-len(busy) {
			return booking.BusySnapshot{}, booking.ErrUnavailable
		}
		busy = append(busy, periods...)
	}
	current := c.now().UTC()
	if ctx.Err() != nil || current.Before(observed) || current.Sub(observed) > booking.MaxSnapshotAge {
		return booking.BusySnapshot{}, booking.ErrUnavailable
	}
	return booking.BusySnapshot{ObservedAt: observed, Coverage: coverage, Complete: true, Busy: busy}, nil
}

func (c *Client) validSources() bool {
	return c != nil && c.origin != nil && c.origin.Scheme == "https" && c.origin.Hostname() != "" && c.origin.User == nil && c.origin.RawQuery == "" && c.origin.Fragment == "" && c.origin.Opaque == "" && (c.httpClient != nil || c.network != nil) && c.credentials != nil && c.now != nil && validCollections(c.calendars)
}

func (c *Client) report(ctx context.Context, client *http.Client, collection string, data []byte, coverage booking.Interval) ([]booking.Interval, error) {
	req, err := c.sourceRequest(ctx, "REPORT", collection, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Depth", "1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, booking.ErrUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := readCalendarBody(ctx, resp, maxReportBytes)
	if err != nil {
		return nil, err
	}
	return readFreeBusy(body, coverage)
}

func readFreeBusy(body []byte, coverage booking.Interval) ([]booking.Interval, error) {
	decoder := ical.NewDecoder(bytes.NewReader(body))
	calendar, err := decoder.Decode()
	if err != nil || calendar == nil || len(calendar.Children) != 1 || calendar.Children[0].Name != ical.CompFreeBusy {
		return nil, booking.ErrUnavailable
	}
	if _, err := decoder.Decode(); !errors.Is(err, io.EOF) {
		return nil, booking.ErrUnavailable
	}
	component := calendar.Children[0]
	from, err := utcProperty(component.Props, ical.PropDateTimeStart)
	if err != nil || from.After(coverage.Start) {
		return nil, booking.ErrUnavailable
	}
	until, err := utcProperty(component.Props, ical.PropDateTimeEnd)
	if err != nil || until.Before(coverage.End) || !from.Before(until) {
		return nil, booking.ErrUnavailable
	}
	for _, recurrence := range []string{"RRULE", "RDATE", "EXDATE", "EXRULE"} {
		if len(component.Props.Values(recurrence)) != 0 {
			return nil, booking.ErrUnavailable
		}
	}
	busy := make([]booking.Interval, 0)
	periodCount := 0
	for _, property := range component.Props.Values(ical.PropFreeBusy) {
		if property.ValueType() != ical.ValuePeriod || len(property.Params.Values("FBTYPE")) > 1 {
			return nil, booking.ErrUnavailable
		}
		kind := strings.ToUpper(property.Params.Get("FBTYPE"))
		if kind != "" && kind != "FREE" && kind != "BUSY" && kind != "BUSY-TENTATIVE" && kind != "BUSY-UNAVAILABLE" {
			return nil, booking.ErrUnavailable
		}
		for value := range strings.SplitSeq(property.Value, ",") {
			periodCount++
			if periodCount > maxBusyPeriods {
				return nil, booking.ErrUnavailable
			}
			period, err := utcPeriod(value)
			if err != nil {
				return nil, booking.ErrUnavailable
			}
			if kind == "FREE" || !period.Start.Before(coverage.End) || !coverage.Start.Before(period.End) {
				continue
			}
			if period.Start.Before(coverage.Start) {
				period.Start = coverage.Start
			}
			if period.End.After(coverage.End) {
				period.End = coverage.End
			}
			busy = append(busy, period)
		}
	}
	return busy, nil
}

func utcProperty(properties ical.Props, name string) (time.Time, error) {
	values := properties.Values(name)
	if len(values) != 1 || values[0].ValueType() != ical.ValueDateTime || values[0].Params.Get(ical.PropTimezoneID) != "" {
		return time.Time{}, booking.ErrUnavailable
	}
	return utcValue(values[0].Value)
}

func utcValue(value string) (time.Time, error) {
	if len(value) != 16 || !strings.HasSuffix(value, "Z") {
		return time.Time{}, booking.ErrUnavailable
	}
	property := ical.NewProp(ical.PropDateTimeStart)
	property.Value = value
	return property.DateTime(time.UTC)
}

func utcPeriod(value string) (booking.Interval, error) {
	start, end, found := strings.Cut(value, "/")
	if !found || strings.Contains(end, "/") {
		return booking.Interval{}, booking.ErrUnavailable
	}
	from, err := utcValue(start)
	if err != nil {
		return booking.Interval{}, booking.ErrUnavailable
	}
	var until time.Time
	if strings.HasPrefix(end, "P") {
		if !periodDuration.MatchString(end) {
			return booking.Interval{}, booking.ErrUnavailable
		}
		property := ical.NewProp(ical.PropDuration)
		property.Value = end
		duration, err := property.Duration()
		if err != nil || duration <= 0 || duration > 366*24*time.Hour {
			return booking.Interval{}, booking.ErrUnavailable
		}
		until = from.Add(duration)
	} else {
		until, err = utcValue(end)
		if err != nil {
			return booking.Interval{}, booking.ErrUnavailable
		}
	}
	if !from.Before(until) {
		return booking.Interval{}, booking.ErrUnavailable
	}
	return booking.Interval{Start: from, End: until}, nil
}
