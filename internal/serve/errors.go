package serve

import (
	"errors"
)

var (
	errBadRequest = errors.New("bad request")
	errNotFound   = errors.New("not found")
	errConflict   = errors.New("conflict")
)
