// kita-pickup-notifier pushes a "time to leave" notification for the
// afternoon kita pickup, based on live BVG departures.
//
// It is meant to run every 5 minutes in the pickup window (Cloud Scheduler →
// Cloud Run job). Each run is stateless: it checks whether today's push already
// went out (via ntfy's cache), asks BVG for the latest trip that still arrives
// in time, and sends the push once "now" is within LEAD_MIN of leaving.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	FromLat, FromLon, FromName string
	ToLat, ToLon, ToName       string
	ArriveBy                   string        // "15:15", wall clock Europe/Berlin
	Buffer                     time.Duration // aim to arrive this much before ArriveBy
	Lead                       time.Duration // push this long before leaving
	FallbackLeave              string        // "14:45": used if BVG is down, empty = off
	SkipDates                  []string      // YYYY-MM-DD, e.g. kita closing days
	NtfyServer, NtfyTopic      string
	BVGAPI                     string
	DryRun                     bool
	Now                        string // override for testing, "2006-01-02T15:04"
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func mustMinutes(k, def string) time.Duration {
	n, err := strconv.Atoi(env(k, def))
	if err != nil {
		log.Fatalf("%s: %v", k, err)
	}
	return time.Duration(n) * time.Minute
}

func loadConfig() Config {
	cfg := Config{
		FromLat:       os.Getenv("FROM_LAT"),
		FromLon:       os.Getenv("FROM_LON"),
		FromName:      env("FROM_NAME", "Home"),
		ToLat:         os.Getenv("TO_LAT"),
		ToLon:         os.Getenv("TO_LON"),
		ToName:        env("TO_NAME", "Kita"),
		ArriveBy:      env("ARRIVE_BY", "15:15"),
		Buffer:        mustMinutes("BUFFER_MIN", "5"),
		Lead:          mustMinutes("LEAD_MIN", "10"),
		FallbackLeave: os.Getenv("FALLBACK_LEAVE"),
		NtfyServer:    strings.TrimRight(env("NTFY_SERVER", "https://ntfy.sh"), "/"),
		NtfyTopic:     os.Getenv("NTFY_TOPIC"),
		BVGAPI:        strings.TrimRight(env("BVG_API", "https://v6.bvg.transport.rest"), "/"),
		DryRun:        os.Getenv("DRY_RUN") == "1",
		Now:           os.Getenv("NOW"),
	}
	if s := os.Getenv("SKIP_DATES"); s != "" {
		for _, d := range strings.Split(s, ",") {
			cfg.SkipDates = append(cfg.SkipDates, strings.TrimSpace(d))
		}
	}
	for k, v := range map[string]string{"FROM_LAT": cfg.FromLat, "FROM_LON": cfg.FromLon, "TO_LAT": cfg.ToLat, "TO_LON": cfg.ToLon, "NTFY_TOPIC": cfg.NtfyTopic} {
		if v == "" {
			log.Fatalf("missing env %s", k)
		}
	}
	return cfg
}

// atClock returns today's date (in now's location) at "HH:MM".
func atClock(now time.Time, hhmm string) (time.Time, error) {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad time %q: %w", hhmm, err)
	}
	y, m, d := now.Date()
	return time.Date(y, m, d, t.Hour(), t.Minute(), 0, 0, now.Location()), nil
}

// skipReason says why no pickup push is needed today ("" = workday).
func skipReason(now time.Time, skip []string) string {
	switch now.Weekday() {
	case time.Saturday, time.Sunday:
		return "weekend"
	}
	if n, ok := berlinHoliday(now); ok {
		return "public holiday: " + n
	}
	if slices.Contains(skip, now.Format("2006-01-02")) {
		return "listed in SKIP_DATES"
	}
	return ""
}

// shouldSend: push once we are within lead of leaving (or already late).
func shouldSend(now, leave time.Time, lead time.Duration) bool {
	return !now.Before(leave.Add(-lead))
}

func hm(t time.Time) string { return t.Format("15:04") }

func buildMessage(cfg Config, j Journey, now time.Time) Message {
	ride := j.FirstRide()
	leave := j.LeaveAt().In(now.Location())
	title := "Leave for Kita at " + hm(leave)
	prio := "high"
	if !now.Before(leave) {
		title = "Leave for Kita NOW"
		prio = "urgent"
	}
	delay := ""
	if ride.DepartureDelay != nil && *ride.DepartureDelay >= 60 {
		delay = fmt.Sprintf(" (+%d min late)", *ride.DepartureDelay/60)
	}
	platform := ""
	if ride.DeparturePlatfm != "" {
		platform = ", platform " + ride.DeparturePlatfm
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s from %s%s%s\n", ride.Line.Name, hm(ride.dep().In(now.Location())), ride.Origin.Name, platform, delay)
	for i := range j.Legs {
		l := &j.Legs[i]
		if !l.Walking && l != ride && l.Line != nil {
			fmt.Fprintf(&b, "then %s %s from %s\n", l.Line.Name, hm(l.dep().In(now.Location())), l.Origin.Name)
		}
	}
	fmt.Fprintf(&b, "Arrive at kita %s", hm(j.ArriveAt().In(now.Location())))
	return Message{
		Title:    title,
		Body:     b.String(),
		Priority: prio,
		Tags:     []string{"tram", sentTag},
		Click:    mapsLink(cfg),
	}
}

func mapsLink(cfg Config) string {
	q := url.Values{}
	q.Set("api", "1")
	q.Set("origin", cfg.FromLat+","+cfg.FromLon)
	q.Set("destination", cfg.ToLat+","+cfg.ToLon)
	q.Set("travelmode", "transit")
	return "https://www.google.com/maps/dir/?" + q.Encode()
}

func run(ctx context.Context, cfg Config) error {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return err
	}
	now := time.Now().In(loc)
	if cfg.Now != "" {
		if now, err = time.ParseInLocation("2006-01-02T15:04", cfg.Now, loc); err != nil {
			return fmt.Errorf("NOW: %w", err)
		}
	}
	if r := skipReason(now, cfg.SkipDates); r != "" {
		log.Printf("no pickup today (%s)", r)
		return nil
	}

	c := &http.Client{Timeout: 20 * time.Second}
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if !cfg.DryRun {
		sent, err := alreadySent(ctx, c, cfg, midnight)
		if err != nil {
			return fmt.Errorf("checking ntfy: %w", err)
		}
		if sent {
			log.Printf("push already sent today")
			return nil
		}
	}

	arriveBy, err := atClock(now, cfg.ArriveBy)
	if err != nil {
		return err
	}
	deadline := arriveBy.Add(-cfg.Buffer)

	journeys, err := fetchJourneys(ctx, c, cfg, deadline)
	if err != nil {
		return fallback(ctx, c, cfg, now, err)
	}
	j, ok := pickJourney(journeys, deadline)
	if !ok {
		return fallback(ctx, c, cfg, now, fmt.Errorf("no usable journey among %d", len(journeys)))
	}
	leave := j.LeaveAt().In(loc)
	ride := j.FirstRide()
	log.Printf("plan: leave %s, %s %s from %s, arrive %s (deadline %s)",
		hm(leave), ride.Line.Name, hm(ride.dep().In(loc)), ride.Origin.Name, hm(j.ArriveAt().In(loc)), hm(deadline))

	if !shouldSend(now, leave, cfg.Lead) {
		log.Printf("too early: push at %s", hm(leave.Add(-cfg.Lead)))
		return nil
	}
	return send(ctx, c, cfg, buildMessage(cfg, j, now))
}

// fallback sends a generic reminder if BVG data is unavailable and we are
// past FALLBACK_LEAVE minus lead; otherwise it just reports the error.
func fallback(ctx context.Context, c *http.Client, cfg Config, now time.Time, cause error) error {
	if cfg.FallbackLeave == "" {
		return cause
	}
	leave, err := atClock(now, cfg.FallbackLeave)
	if err != nil {
		return err
	}
	if !shouldSend(now, leave, cfg.Lead) {
		return fmt.Errorf("%w (fallback push at %s)", cause, hm(leave.Add(-cfg.Lead)))
	}
	log.Printf("BVG unavailable (%v), sending fallback", cause)
	return send(ctx, c, cfg, Message{
		Title:    "Leave for Kita at " + hm(leave),
		Body:     "No live BVG data right now - leaving at the usual time. Check the BVG app.",
		Priority: "high",
		Tags:     []string{"warning", sentTag},
		Click:    mapsLink(cfg),
	})
}

func send(ctx context.Context, c *http.Client, cfg Config, m Message) error {
	if cfg.DryRun {
		log.Printf("DRY RUN push [%s] %s\n%s", m.Priority, m.Title, m.Body)
		return nil
	}
	if err := publish(ctx, c, cfg, m); err != nil {
		return err
	}
	log.Printf("push sent: %s", m.Title)
	return nil
}

func main() {
	log.SetFlags(0)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := run(ctx, loadConfig()); err != nil {
		log.Fatalf("error: %v", err)
	}
}
