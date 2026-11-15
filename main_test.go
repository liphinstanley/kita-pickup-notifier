package main

import (
	"testing"
	"time"
)

var berlin, _ = time.LoadLocation("Europe/Berlin")

func at(hhmm string) *time.Time {
	t, _ := time.ParseInLocation("2006-01-02 15:04", "2026-11-16 "+hhmm, berlin)
	return &t
}

func journey(leave, rideDep, arrive string, cancelled bool) Journey {
	return Journey{Legs: []Leg{
		{Walking: true, Departure: at(leave), Arrival: at(rideDep)},
		{Line: &Line{Name: "M17"}, Departure: at(rideDep), Arrival: at(arrive), Cancelled: cancelled},
	}}
}

func TestPickJourney(t *testing.T) {
	deadline := *at("15:10")
	walkOnly := Journey{Legs: []Leg{{Walking: true, Departure: at("14:59"), Arrival: at("15:09")}}}
	tests := []struct {
		name      string
		in        []Journey
		wantLeave string
		wantOK    bool
	}{
		{"latest that fits", []Journey{journey("14:48", "14:50", "15:00", false), journey("14:58", "15:00", "15:10", false)}, "14:58", true},
		{"too late is dropped", []Journey{journey("14:48", "14:50", "15:00", false), journey("15:03", "15:05", "15:12", false)}, "14:48", true},
		{"cancelled is dropped", []Journey{journey("14:48", "14:50", "15:00", false), journey("14:58", "15:00", "15:10", true)}, "14:48", true},
		{"walk only is dropped", []Journey{walkOnly, journey("14:48", "14:50", "15:00", false)}, "14:48", true},
		{"nothing usable", []Journey{walkOnly}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, ok := pickJourney(tt.in, deadline)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && hm(j.LeaveAt()) != tt.wantLeave {
				t.Errorf("leave = %s, want %s", hm(j.LeaveAt()), tt.wantLeave)
			}
		})
	}
}

func TestShouldSend(t *testing.T) {
	leave := *at("14:48")
	for now, want := range map[string]bool{"14:30": false, "14:37": false, "14:38": true, "14:45": true, "14:50": true} {
		if got := shouldSend(*at(now), leave, 10*time.Minute); got != want {
			t.Errorf("now %s: got %v want %v", now, got, want)
		}
	}
}

func TestSkipReason(t *testing.T) {
	d := func(s string) time.Time { t, _ := time.ParseInLocation("2006-01-02", s, berlin); return t }
	tests := map[string]bool{ // date -> skipped?
		"2026-11-16": false, // Monday
		"2026-11-14": true,  // Saturday
		"2026-04-03": true,  // Karfreitag 2026
		"2026-04-06": true,  // Ostermontag 2026
		"2026-05-14": true,  // Himmelfahrt 2026
		"2026-05-25": true,  // Pfingstmontag 2026
		"2027-03-08": true,  // Frauentag (Monday)
		"2026-12-25": true,
		"2026-12-23": true, // in SKIP_DATES below
		"2026-12-22": false,
	}
	for date, want := range tests {
		if got := skipReason(d(date), []string{"2026-12-23"}) != ""; got != want {
			t.Errorf("%s: skipped=%v want %v", date, got, want)
		}
	}
}
