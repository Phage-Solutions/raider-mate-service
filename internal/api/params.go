package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"
)

// pathUUID parses a path value as a UUID.
func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s: %w", name, err)
	}
	return id, nil
}

// pathSnowflake parses a path value as a Discord snowflake.
func pathSnowflake(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return id, nil
}
