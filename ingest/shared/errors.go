package shared

import "errors"

// ErrNotFound is returned (wrapped) by an HTTPGetFunc when the server responds 404 Not Found — a
// definitive "this resource does not exist" that callers distinguish from a transient transport
// failure (connection reset, EOF, timeout). A 404 must NOT be retried; a transient error may be.
var ErrNotFound = errors.New("shared: resource not found (404)")
