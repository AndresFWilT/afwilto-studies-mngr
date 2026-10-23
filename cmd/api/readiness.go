package main

import "github.com/afwilto/studies-mngr/internal/platform/httpx"

// newReadiness is what /readyz asks: the database answers and the media
// directory takes writes (slice 05 §1.7).
func newReadiness(db, media httpx.Pinger) httpx.Pinger {
	return httpx.Readiness(
		httpx.Check{Name: "database", Pinger: db},
		httpx.Check{Name: "media storage", Pinger: media},
	)
}
