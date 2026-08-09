package ops

import (
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
)

// The operational modules all translate the same three database outcomes into
// the same three API answers. Sharing them keeps the wording — and, more
// importantly, the choice of 404 over 403 for out-of-scope objects (§9) —
// consistent across ten modules.

// NotFoundOr maps a missing row to a 404 for the named resource and anything
// else to a 500.
func NotFoundOr(err error, resource string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFound(resource)
	}
	return apierr.Internal(err)
}

// ConflictOr maps a compare-and-swap that matched no rows to a retryable
// conflict. Every operational UPDATE is guarded by an expected status, so zero
// rows means somebody else changed the record between the read and the write.
func ConflictOr(err error, message string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.Conflict(apierr.CodeConcurrentModification, message)
	}
	return apierr.Internal(err)
}

// IsNoRows reports whether an error is a missing row.
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// IsUnique reports whether an error is a violation of a named unique index.
func IsUnique(err error, constraint string) bool {
	return database.IsUniqueViolation(err, constraint)
}

// TriggerMessage returns the message a database trigger raised, which the
// bagging and manifest guards use to explain a frozen-contents refusal in the
// words the schema chose rather than a generic 500.
func TriggerMessage(err error) (string, bool) {
	msg := database.RaisedMessage(err)
	return msg, msg != ""
}
