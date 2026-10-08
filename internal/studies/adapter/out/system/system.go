// Package system provides the production Clock and IDGenerator adapters.
package system

import (
	"time"

	"github.com/google/uuid"
)

type Clock struct{}

func (Clock) Now() time.Time { return time.Now().UTC() }

// UUIDv7Generator yields time-ordered ids for B-tree locality (§7.2).
type UUIDv7Generator struct{}

func (UUIDv7Generator) NewID() string {
	id, err := uuid.NewV7()
	if err != nil {
		// NewV7 only fails if the entropy source does; fall back to v4.
		return uuid.NewString()
	}
	return id.String()
}
