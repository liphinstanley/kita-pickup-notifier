package main

import "time"

// easter returns Easter Sunday (Gregorian, anonymous algorithm).
func easter(year int, loc *time.Location) time.Time {
	a := year % 19
	b, c := year/100, year%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, loc)
}

// berlinHoliday reports whether d is a public holiday in Berlin, and its name.
func berlinHoliday(d time.Time) (string, bool) {
	y, m, day := d.Date()
	fixed := map[[2]int]string{
		{1, 1}:   "Neujahr",
		{3, 8}:   "Frauentag",
		{5, 1}:   "Tag der Arbeit",
		{10, 3}:  "Tag der Deutschen Einheit",
		{12, 25}: "1. Weihnachtstag",
		{12, 26}: "2. Weihnachtstag",
	}
	if n, ok := fixed[[2]int{int(m), day}]; ok {
		return n, true
	}
	e := easter(y, d.Location())
	movable := map[int]string{
		-2: "Karfreitag",
		1:  "Ostermontag",
		39: "Christi Himmelfahrt",
		50: "Pfingstmontag",
	}
	today := time.Date(y, m, day, 0, 0, 0, 0, d.Location())
	diff := int(today.Sub(e).Hours() / 24)
	if n, ok := movable[diff]; ok {
		return n, true
	}
	return "", false
}
