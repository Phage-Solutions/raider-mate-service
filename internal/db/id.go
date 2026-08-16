package db

import "github.com/google/uuid"

// NewID returns a primary key. Every uuid PK in the schema is supplied by the
// application, so no table carries a DEFAULT and a caller that forgets one gets a
// not-null violation rather than a silently mismatched id.
//
// uuid.NewV7 fails only when crypto/rand does, which Go already treats as fatal, so
// there is no error worth threading through every insert. google/uuid serialises
// generation behind a mutex and bumps a 12-bit sequence, so ids from one process are
// strictly increasing even within a millisecond.
func NewID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}
