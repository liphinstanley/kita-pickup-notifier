package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Subset of the transport.rest (hafas-client) journey format we need.
type journeysResponse struct {
	Journeys []Journey `json:"journeys"`
}

type Journey struct {
	Legs []Leg `json:"legs"`
}

type Leg struct {
	Origin          Place      `json:"origin"`
	Destination     Place      `json:"destination"`
	Departure       *time.Time `json:"departure"` // realtime if available, nil if cancelled
	PlannedDep      *time.Time `json:"plannedDeparture"`
	DepartureDelay  *int       `json:"departureDelay"` // seconds
	Arrival         *time.Time `json:"arrival"`
	PlannedArrival  *time.Time `json:"plannedArrival"`
	Walking         bool       `json:"walking"`
	Cancelled       bool       `json:"cancelled"`
	Line            *Line      `json:"line"`
	DeparturePlatfm string     `json:"departurePlatform"`
}

type Place struct {
	Name string `json:"name"`
}

type Line struct {
	Name    string `json:"name"`
	Product string `json:"product"`
}

func (l Leg) dep() time.Time {
	if l.Departure != nil {
		return *l.Departure
	}
	if l.PlannedDep != nil {
		return *l.PlannedDep
	}
	return time.Time{}
}

func (l Leg) arr() time.Time {
	if l.Arrival != nil {
		return *l.Arrival
	}
	if l.PlannedArrival != nil {
		return *l.PlannedArrival
	}
	return time.Time{}
}

// LeaveAt is when she has to walk out of the door (start of the first leg).
func (j Journey) LeaveAt() time.Time { return j.Legs[0].dep() }

// ArriveAt is when she reaches the kita (end of the last leg).
func (j Journey) ArriveAt() time.Time { return j.Legs[len(j.Legs)-1].arr() }

// FirstRide is the first non-walking leg, nil for walk-only journeys.
func (j Journey) FirstRide() *Leg {
	for i := range j.Legs {
		if !j.Legs[i].Walking {
			return &j.Legs[i]
		}
	}
	return nil
}

func (j Journey) cancelled() bool {
	for _, l := range j.Legs {
		if l.Cancelled {
			return true
		}
	}
	return false
}

// pickJourney returns the journey that leaves home as late as possible while
// still arriving by deadline. Walk-only and cancelled journeys are ignored.
func pickJourney(js []Journey, deadline time.Time) (Journey, bool) {
	var best Journey
	found := false
	for _, j := range js {
		if len(j.Legs) == 0 || j.cancelled() || j.FirstRide() == nil {
			continue
		}
		if j.LeaveAt().IsZero() || j.ArriveAt().IsZero() || j.ArriveAt().After(deadline) {
			continue
		}
		if !found || j.LeaveAt().After(best.LeaveAt()) ||
			(j.LeaveAt().Equal(best.LeaveAt()) && j.ArriveAt().Before(best.ArriveAt())) {
			best, found = j, true
		}
	}
	return best, found
}

func fetchJourneys(ctx context.Context, c *http.Client, cfg Config, arriveBy time.Time) ([]Journey, error) {
	q := url.Values{}
	q.Set("from.latitude", cfg.FromLat)
	q.Set("from.longitude", cfg.FromLon)
	q.Set("from.address", cfg.FromName)
	q.Set("to.latitude", cfg.ToLat)
	q.Set("to.longitude", cfg.ToLon)
	q.Set("to.address", cfg.ToName)
	q.Set("arrival", arriveBy.Format(time.RFC3339))
	q.Set("results", "6")
	q.Set("stopovers", "false")
	q.Set("remarks", "false")
	q.Set("language", "en")

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 3 * time.Second)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BVGAPI+"/journeys?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "kita-pickup-notifier (personal, 1 req/5min)")
		resp, err := c.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		var body journeysResponse
		err = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("bvg api: HTTP %d", resp.StatusCode)
			continue
		}
		if err != nil {
			lastErr = fmt.Errorf("bvg api: decode: %w", err)
			continue
		}
		return body.Journeys, nil
	}
	return nil, lastErr
}
