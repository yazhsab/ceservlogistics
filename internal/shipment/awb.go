package shipment

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
)

// AWB layout: a 2-4 character tenant prefix, a six-digit YYMMDD period, and a
// six-digit zero-padded sequence that resets daily.
//
//	QYN 260808 000001
//
// The prefix namespaces tenants so the AWB can be globally unique, which lets
// public tracking accept a bare AWB with no tenant hint.
const (
	awbSequenceWidth = 6
	awbMaxPerPeriod  = 999_999
)

var awbPattern = regexp.MustCompile(`^[A-Z]{2,4}[0-9]{6}[0-9]{6}$`)

// ValidAWB reports whether a string has the AWB shape.
func ValidAWB(awb string) bool { return awbPattern.MatchString(awb) }

// Allocator issues AWB numbers.
//
// Constitution §11 and §22: allocation must be concurrency-safe across
// instances and must not depend on a process-local lock. The allocator holds no
// state at all — the counter lives in awb_sequences and is advanced by a single
// atomic UPSERT, so correctness comes from PostgreSQL's row locking rather than
// from anything this process remembers.
type Allocator struct {
	q *dbgen.Queries
}

// NewAllocator builds an AWB allocator.
func NewAllocator(q *dbgen.Queries) *Allocator { return &Allocator{q: q} }

// Allocate issues the next AWB for a tenant.
//
// It deliberately runs OUTSIDE the booking transaction. Holding the sequence
// row for the whole booking would serialise every concurrent booking in the
// tenant behind one row lock; taking it in its own short statement keeps the
// contended window to microseconds. The cost is that a failed booking may leave
// a gap in the series, which is acceptable — an AWB is an identifier, not a
// gapless accounting sequence — and shipments.awb carries a UNIQUE constraint
// so a duplicate is impossible even if this allocator were wrong.
func (a *Allocator) Allocate(ctx context.Context, orgID int64, prefix string, at time.Time) (string, error) {
	period := at.UTC().Format("060102")
	row, err := a.q.AllocateAWBNumber(ctx, dbgen.AllocateAWBNumberParams{
		OrganizationID: orgID,
		Prefix:         prefix,
		PeriodKey:      period,
		MaxValue:       awbMaxPerPeriod,
	})
	if err != nil {
		return "", apierr.Internal(fmt.Errorf("allocate AWB: %w", err))
	}
	if row.CurrentValue > row.MaxValue {
		// The daily series is exhausted. Failing loudly is far better than
		// wrapping and colliding with this morning's shipments.
		return "", apierr.Conflict(apierr.CodeConflict,
			"The AWB series for today is exhausted. Contact platform operations to extend the numbering range.").
			WithDetail("prefix", prefix).WithDetail("period", period)
	}
	return fmt.Sprintf("%s%s%0*d", prefix, period, awbSequenceWidth, row.CurrentValue), nil
}

// PieceBarcode derives a scannable per-piece barcode from the AWB.
//
// Pieces are scanned individually during bagging and delivery, so each needs
// its own identifier that still reads back to the parent AWB.
func PieceBarcode(awb string, sequence int) string {
	return fmt.Sprintf("%s-%02d", awb, sequence)
}
