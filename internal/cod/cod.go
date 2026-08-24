// Package cod implements M23: cash-on-delivery custody and liability.
//
// COD is a custody problem before it is an accounting problem. Real cash moves
//
//	consignee → agent → branch → franchise → head office → consignor
//
// and at every hop somebody is liable for it. The Constitution (§25) forbids
// modelling that as an editable balance, so nothing here stores "how much is
// owed" as a mutable number that a bug or a keystroke could change. Instead:
//
//   - cod_obligations carries the custody *pointer* (who holds it now) and the
//     immutable expected amount fixed at booking.
//   - Every movement writes a row — a collection, a transfer, a reconciliation,
//     a remittance — and posts the matching journal.
//   - "What is this branch holding?" is a SUM over obligations, and "what is it
//     worth?" is a SUM over ledger entries. The two are independently derived
//     and must agree; the integration tests check that they do.
//
// # The accounting
//
// Each hop moves a receivable from one party's subsidiary account to the next,
// which is what makes "who is liable" answerable from the ledger alone:
//
//	collection    DR 1100 COD Receivable — agent      CR 2000 COD Payable to consignor
//	agent→branch  DR 1110 COD Receivable — branch     CR 1100 COD Receivable — agent
//	branch→frn    DR 1120 COD Receivable — franchise  CR 1110 COD Receivable — branch
//	shortage      DR 1400 COD Shortage Recoverable    CR 11x0 COD Receivable — holder
//	excess        DR 11x0 COD Receivable — holder     CR 2500 COD Excess Payable
//	remit to HO   DR 1000 Cash and Bank               CR 11x0 COD Receivable — holder
//	pay consignor DR 2000 COD Payable to consignor    CR 1000 Cash and Bank
//	write off     DR 5200 COD Shortage Written Off    CR 1400 COD Shortage Recoverable
//
// The liability to the consignor (2000) is opened at collection and closed only
// when the sender is actually paid. It is deliberately independent of where the
// cash currently sits: the consignee's money is owed to the sender from the
// moment it is taken, whoever happens to be carrying it.
package cod

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ledger"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/telemetry"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Obligation statuses — the custody lifecycle.
const (
	StatusExpected           = "EXPECTED"
	StatusAgentCollected     = "AGENT_COLLECTED"
	StatusBranchReceived     = "BRANCH_RECEIVED"
	StatusFranchiseConfirmed = "FRANCHISE_CONFIRMED"
	StatusReconciled         = "RECONCILED"
	StatusRemitted           = "REMITTED"
	StatusClosed             = "CLOSED"
	StatusCancelled          = "CANCELLED"
	StatusWrittenOff         = "WRITTEN_OFF"
)

// Custodian and party types.
const (
	PartyAgent      = "AGENT"
	PartyUnit       = "OPERATING_UNIT"
	PartyFranchise  = "FRANCHISE"
	PartyHeadOffice = "HEAD_OFFICE"
	PartyCustomer   = "CUSTOMER"
)

// Transfer statuses.
const (
	TransferDeclared  = "DECLARED"
	TransferAccepted  = "ACCEPTED"
	TransferDisputed  = "DISPUTED"
	TransferCancelled = "CANCELLED"
)

// Adjustment types.
const (
	AdjShortageRecovery  = "SHORTAGE_RECOVERY"
	AdjShortageWriteOff  = "SHORTAGE_WRITE_OFF"
	AdjExcessRefund      = "EXCESS_REFUND"
	AdjExcessRetained    = "EXCESS_RETAINED"
	AdjWaiver            = "WAIVER"
	AdjCorrection        = "CORRECTION"
	AdjDisputeResolution = "DISPUTE_RESOLUTION"
)

// Error codes.
const (
	CodeNotCOD           = "COD_NOT_APPLICABLE"
	CodeAlreadyCollected = "COD_ALREADY_COLLECTED"
	CodeWrongCustodian   = "COD_WRONG_CUSTODIAN"
	CodeInvalidState     = "COD_INVALID_STATE"
	CodeAmountMismatch   = "COD_AMOUNT_MISMATCH"
	CodeTransferClosed   = "COD_TRANSFER_CLOSED"
	CodeSelfApproval     = "COD_SELF_APPROVAL"
	CodeNotApproved      = "COD_NOT_APPROVED"
	CodeActiveDispute    = "COD_ACTIVE_DISPUTE"
	CodeNothingToRemit   = "COD_NOTHING_TO_REMIT"
)

// Service owns COD custody and its ledger effects.
type Service struct {
	db                  *database.DB
	q                   *dbgen.Queries
	ledger              *ledger.Service
	audit               *audit.Recorder
	log                 *slog.Logger
	metrics             *telemetry.Metrics
	collectionObservers []CollectionObserver
}

// CollectionObserver receives a newly committed-to-the-transaction COD
// collection. It runs before commit and must only enqueue durable work.
type CollectionObserver func(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, result *CollectResult,
) error

// ObserveCollection registers a COD collection outbox observer at startup.
func (s *Service) ObserveCollection(observer CollectionObserver) {
	if observer != nil {
		s.collectionObservers = append(s.collectionObservers, observer)
	}
}

func (s *Service) runCollectionObservers(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, result *CollectResult,
) error {
	for _, observer := range s.collectionObservers {
		if err := observer(ctx, tx, p, result); err != nil {
			return err
		}
	}
	return nil
}

func NewService(
	db *database.DB, q *dbgen.Queries, led *ledger.Service,
	rec *audit.Recorder, log *slog.Logger, m *telemetry.Metrics) *Service {
	return &Service{db: db, q: q, ledger: led, audit: rec, log: log, metrics: m}
}

// receivableAccountFor returns the control account that holds a party's COD.
func receivableAccountFor(partyType string) (string, string, error) {
	switch partyType {
	case PartyAgent:
		return ledger.AcctCODReceivableAgent, ledger.PartyAgent, nil
	case PartyUnit:
		return ledger.AcctCODReceivableBranch, ledger.PartyOperatingUnit, nil
	case PartyFranchise:
		return ledger.AcctCODReceivableFranchise, ledger.PartyFranchise, nil
	}
	return "", "", apierr.Validation("Unknown COD custodian type.",
		map[string]any{"custodianType": partyType,
			"allowed": []string{PartyAgent, PartyUnit, PartyFranchise}})
}

// statusForCustodian maps a holder to the obligation status that means "this
// party has it".
func statusForCustodian(partyType string) (string, error) {
	switch partyType {
	case PartyAgent:
		return StatusAgentCollected, nil
	case PartyUnit:
		return StatusBranchReceived, nil
	case PartyFranchise:
		return StatusFranchiseConfirmed, nil
	}
	return "", apierr.Validation("Unknown COD custodian type.",
		map[string]any{"custodianType": partyType})
}

// ---------------------------------------------------------------------------
// Obligations
// ---------------------------------------------------------------------------

// OpenObligation records that a shipment is expected to collect cash.
//
// Called from booking. Idempotent on shipment: the unique index on shipment_id
// means a retried booking cannot create a second liability for one parcel.
func (s *Service) OpenObligation(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, sh dbgen.Shipment,
) (*dbgen.CodObligation, error) {
	q := s.q.WithTx(tx)

	if sh.CodAmountMinor <= 0 {
		return nil, apierr.Conflict(CodeNotCOD,
			fmt.Sprintf("Shipment %s carries no COD amount.", sh.Awb))
	}

	// Already open? Return it rather than colliding on the index.
	if existing, err := q.GetObligationByShipment(ctx, dbgen.GetObligationByShipmentParams{
		OrganizationID: p.OrganizationID, ShipmentID: sh.ID,
	}); err == nil {
		return &existing, nil
	} else if !ops.IsNoRows(err) {
		return nil, apierr.Internal(err)
	}

	originFranchise, destFranchise := s.franchisesFor(ctx, q, p.OrganizationID, sh)

	obligation, err := q.CreateCODObligation(ctx, dbgen.CreateCODObligationParams{
		PublicID:               publicid.New("cod"),
		OrganizationID:         p.OrganizationID,
		ShipmentID:             sh.ID,
		Awb:                    sh.Awb,
		ExpectedMinor:          sh.CodAmountMinor,
		Currency:               sh.Currency,
		ConsignorCustomerID:    &sh.CustomerID,
		OriginFranchiseID:      originFranchise,
		DestinationFranchiseID: destFranchise,
		Status:                 ops.Optional(StatusExpected),
		Metadata:               []byte("{}"),
	})
	if err != nil {
		if ops.IsUnique(err, "cod_obligations_shipment_idx") {
			again, gErr := q.GetObligationByShipment(ctx, dbgen.GetObligationByShipmentParams{
				OrganizationID: p.OrganizationID, ShipmentID: sh.ID,
			})
			if gErr == nil {
				return &again, nil
			}
		}
		return nil, apierr.Internal(err)
	}
	return &obligation, nil
}

// franchisesFor resolves the franchises that own the origin and destination
// branches. Best effort: a shipment routed through company-owned units has no
// franchise, which is normal and not an error.
func (s *Service) franchisesFor(
	ctx context.Context, q *dbgen.Queries, orgID int64, sh dbgen.Shipment,
) (origin, destination *int64) {
	if sh.OriginBranchID != nil {
		if f, err := q.GetFranchiseByOperatingUnit(ctx, dbgen.GetFranchiseByOperatingUnitParams{
			OperatingUnitID: *sh.OriginBranchID, OrganizationID: orgID}); err == nil {
			id := f.ID
			origin = &id
		}
	}
	if sh.DestinationBranchID != nil {
		if f, err := q.GetFranchiseByOperatingUnit(ctx, dbgen.GetFranchiseByOperatingUnitParams{
			OperatingUnitID: *sh.DestinationBranchID, OrganizationID: orgID}); err == nil {
			id := f.ID
			destination = &id
		}
	}
	return origin, destination
}

// ---------------------------------------------------------------------------
// Collection
// ---------------------------------------------------------------------------

// CollectRequest records cash taken at the doorstep.
type CollectRequest struct {
	ShipmentID        int64
	DeliveryAttemptID *int64
	AmountMinor       int64
	PaymentMode       string
	Reference         string
	CollectedAt       time.Time
	OperatingUnitID   *int64
	// The agent who took the money. Defaults to the acting user.
	AgentUserID *int64
	Device      ops.Device
}

// CollectResult reports what happened, including a replay.
type CollectResult struct {
	Obligation dbgen.CodObligation
	Collection dbgen.CodCollection
	// Duplicate is true when this call replayed an earlier collection, which is
	// the expected outcome of a delivery app retrying over a bad connection.
	Duplicate bool
}

// RecordCollection banks a COD payment and posts the ledger effect.
//
// Three separate idempotency guards, because this is the single most retried
// finance operation in the platform — it runs on a handheld, at a doorstep,
// over a mobile network:
//
//  1. delivery_attempt_id is unique, so one attempt banks once;
//  2. (device_id, device_event_id) is unique, so an offline replay banks once;
//  3. the obligation status CAS refuses a second collection outright.
func (s *Service) RecordCollection(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, in CollectRequest,
) (result *CollectResult, metricErr error) {
	// Counted here, at the one place the outcome is decided, so the
	// metric cannot drift from what actually happened. A named error
	// result makes this correct on every return path rather than only
	// the ones somebody remembered to instrument.
	defer func() {
		if metricErr != nil {
			s.metrics.RecordBusiness("cod_collection", "failed")
			return
		}
		s.metrics.RecordBusiness("cod_collection", "recorded")
	}()

	q := s.q.WithTx(tx)

	if in.AmountMinor <= 0 {
		return nil, apierr.Validation("A COD collection must be for a positive amount.",
			map[string]any{"amountMinor": in.AmountMinor})
	}
	if in.CollectedAt.IsZero() {
		in.CollectedAt = time.Now().UTC()
	}
	agentID := p.UserID
	if in.AgentUserID != nil {
		agentID = *in.AgentUserID
	}

	// Replay probes before anything is written.
	if in.DeliveryAttemptID != nil {
		if existing, err := q.FindCollectionByAttempt(ctx, dbgen.FindCollectionByAttemptParams{
			OrganizationID: p.OrganizationID, DeliveryAttemptID: in.DeliveryAttemptID,
		}); err == nil {
			ob, oErr := q.GetObligationByID(ctx, dbgen.GetObligationByIDParams{
				OrganizationID: p.OrganizationID, ID: existing.ObligationID,
			})
			if oErr != nil {
				return nil, apierr.Internal(oErr)
			}
			return &CollectResult{Obligation: ob, Collection: existing, Duplicate: true}, nil
		} else if !ops.IsNoRows(err) {
			return nil, apierr.Internal(err)
		}
	}
	if in.Device.HasEventKey() {
		if existing, err := q.FindCollectionByDeviceEvent(ctx, dbgen.FindCollectionByDeviceEventParams{
			OrganizationID: p.OrganizationID,
			DeviceID:       ops.Optional(in.Device.ID),
			DeviceEventID:  ops.Optional(in.Device.EventID),
		}); err == nil {
			ob, oErr := q.GetObligationByID(ctx, dbgen.GetObligationByIDParams{
				OrganizationID: p.OrganizationID, ID: existing.ObligationID,
			})
			if oErr != nil {
				return nil, apierr.Internal(oErr)
			}
			return &CollectResult{Obligation: ob, Collection: existing, Duplicate: true}, nil
		} else if !ops.IsNoRows(err) {
			return nil, apierr.Internal(err)
		}
	}

	obligation, err := q.GetObligationByShipment(ctx, dbgen.GetObligationByShipmentParams{
		OrganizationID: p.OrganizationID, ShipmentID: in.ShipmentID,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return nil, apierr.Conflict(CodeNotCOD,
				"This shipment has no COD obligation, so nothing can be collected against it.")
		}
		return nil, apierr.Internal(err)
	}

	// A short or over collection at the door is a real event and is recorded as
	// such; the variance is settled at reconciliation rather than refused here,
	// because the cash has already changed hands.
	if obligation.Status != StatusExpected {
		return nil, apierr.Conflict(CodeAlreadyCollected,
			fmt.Sprintf("COD for %s is already %s.", obligation.Awb, obligation.Status)).
			WithDetail("status", obligation.Status)
	}

	updated, err := q.MarkObligationCollected(ctx, dbgen.MarkObligationCollectedParams{
		OrganizationID: p.OrganizationID, ID: obligation.ID,
		AmountMinor: in.AmountMinor, AgentID: &agentID, CollectedAt: in.CollectedAt,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return nil, apierr.Conflict(CodeAlreadyCollected,
				"This COD was collected by another request.")
		}
		return nil, apierr.Internal(err)
	}

	// DR COD Receivable — agent      the agent now owes the network this cash
	// CR COD Payable to consignor    the network now owes the sender
	posting, err := s.ledger.Post(ctx, tx, p, ledger.Posting{
		SourceType:     ledger.SourceCOD,
		SourceID:       &obligation.ID,
		SourcePublicID: obligation.PublicID,
		Purpose:        "COLLECTION",
		Description:    fmt.Sprintf("COD collected for %s", obligation.Awb),
		Currency:       obligation.Currency,
		PostingDate:    in.CollectedAt,
		Legs: []ledger.Leg{
			{
				AccountCode: ledger.AcctCODReceivableAgent, Debit: in.AmountMinor,
				PartyType: ledger.PartyAgent, PartyID: agentID,
				ShipmentID: &obligation.ShipmentID, OperatingUnitID: in.OperatingUnitID,
				Memo: "Cash held by the delivery agent",
			},
			{
				AccountCode: ledger.AcctCODPayableConsignor, Credit: in.AmountMinor,
				CustomerID: obligation.ConsignorCustomerID,
				ShipmentID: &obligation.ShipmentID,
				Memo:       "Owed to the consignor",
			},
		},
		Metadata: map[string]any{
			"awb": obligation.Awb, "paymentMode": in.PaymentMode,
		},
	})
	if err != nil {
		return nil, err
	}

	collection, err := q.CreateCODCollection(ctx, dbgen.CreateCODCollectionParams{
		PublicID:             publicid.New("cdc"),
		OrganizationID:       p.OrganizationID,
		ObligationID:         obligation.ID,
		ShipmentID:           obligation.ShipmentID,
		DeliveryAttemptID:    in.DeliveryAttemptID,
		AmountMinor:          in.AmountMinor,
		Currency:             obligation.Currency,
		PaymentMode:          in.PaymentMode,
		Reference:            ops.Optional(in.Reference),
		CollectedBy:          &agentID,
		CollectedAt:          in.CollectedAt,
		OperatingUnitID:      in.OperatingUnitID,
		JournalTransactionID: &posting.Transaction.ID,
		DeviceID:             ops.Optional(in.Device.ID),
		DeviceEventID:        ops.Optional(in.Device.EventID),
		RequestID:            ops.Optional(httpx.RequestID(ctx)),
	})
	if err != nil {
		// Lost a race with an identical concurrent collection.
		if ops.IsUnique(err, "cod_collections_attempt_idx") ||
			ops.IsUnique(err, "cod_collections_device_event_idx") {
			return nil, apierr.Conflict(CodeAlreadyCollected,
				"This COD collection was recorded by another request.")
		}
		return nil, apierr.Internal(err)
	}

	result = &CollectResult{Obligation: updated, Collection: collection}
	if err := s.runCollectionObservers(ctx, tx, p, result); err != nil {
		return nil, err
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Custody transfer
// ---------------------------------------------------------------------------

// DeclareTransferRequest is the giver's side of a hand-off.
type DeclareTransferRequest struct {
	FromType string
	FromID   int64
	ToType   string
	ToID     int64
	// Obligations being handed over, by public id.
	ObligationIDs []string
	Notes         string
}

// DeclareTransfer records that a party is handing cash on.
//
// It is only half of the hand-off: until the receiver accepts, the money is
// still the giver's liability. That two-sidedness is the point — an unaccepted
// hand-in is exactly the dispute a real network argues about.
func (s *Service) DeclareTransfer(
	ctx context.Context, p *tenant.Principal, in DeclareTransferRequest,
) (*dbgen.CodCustodyTransfer, []dbgen.ListTransferItemsRow, error) {
	if len(in.ObligationIDs) == 0 {
		return nil, nil, apierr.Validation("A custody transfer needs at least one COD shipment.", nil)
	}
	if in.FromType == in.ToType && in.FromID == in.ToID {
		return nil, nil, apierr.Validation("A transfer cannot be to the same party it is from.", nil)
	}
	expectedStatus, err := statusForCustodian(in.FromType)
	if err != nil {
		return nil, nil, err
	}

	var transfer *dbgen.CodCustodyTransfer
	var items []dbgen.ListTransferItemsRow

	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		ids, err := s.resolveObligationIDs(ctx, q, p.OrganizationID, in.ObligationIDs)
		if err != nil {
			return err
		}

		// Lock in id order so overlapping concurrent transfers serialise
		// instead of deadlocking.
		locked, err := q.LockObligationsByIDs(ctx, dbgen.LockObligationsByIDsParams{
			OrganizationID: p.OrganizationID, Ids: ids,
		})
		if err != nil {
			return apierr.Internal(err)
		}
		if len(locked) != len(ids) {
			return apierr.NotFound("COD obligation")
		}

		var declared int64
		currency := ""
		for _, ob := range locked {
			if ob.Status != expectedStatus {
				return apierr.Conflict(CodeInvalidState,
					fmt.Sprintf("COD for %s is %s and cannot be handed over from a %s.",
						ob.Awb, ob.Status, in.FromType)).
					WithDetail("awb", ob.Awb).WithDetail("status", ob.Status)
			}
			if ob.CustodianType == nil || *ob.CustodianType != in.FromType || ob.CustodianID == nil || *ob.CustodianID != in.FromID {
				return apierr.Conflict(CodeWrongCustodian,
					fmt.Sprintf("COD for %s is not held by this party.", ob.Awb)).
					WithDetail("awb", ob.Awb)
			}
			if currency == "" {
				currency = ob.Currency
			} else if currency != ob.Currency {
				return apierr.Conflict(ledger.CodeCurrencyMismatch,
					"A transfer cannot mix currencies.")
			}
			declared += ob.CollectedMinor - ob.RemittedMinor
		}

		code, err := s.allocateCode(ctx, q, p.OrganizationID, "CDT")
		if err != nil {
			return err
		}

		created, err := q.CreateCustodyTransfer(ctx, dbgen.CreateCustodyTransferParams{
			PublicID:       publicid.New("cdt"),
			OrganizationID: p.OrganizationID,
			TransferCode:   code,
			FromType:       in.FromType, FromID: in.FromID,
			ToType: in.ToType, ToID: in.ToID,
			DeclaredMinor: declared, Currency: currency,
			ShipmentCount: int32(len(locked)),
			DeclaredBy:    &p.UserID,
			Notes:         ops.Optional(in.Notes),
			RequestID:     ops.Optional(httpx.RequestID(ctx)),
		})
		if err != nil {
			return apierr.Internal(err)
		}

		for _, ob := range locked {
			if _, err := q.AddTransferItem(ctx, dbgen.AddTransferItemParams{
				TransferID:   created.ID,
				ObligationID: ob.ID,
				AmountMinor:  ob.CollectedMinor - ob.RemittedMinor,
				Status:       ops.Optional("INCLUDED"),
			}); err != nil {
				// The partial unique index refuses an obligation that is
				// already travelling in another undecided transfer.
				if ops.IsUnique(err, "cod_custody_transfer_items_active_idx") {
					return apierr.Conflict(CodeInvalidState,
						fmt.Sprintf("COD for %s is already in an open custody transfer.", ob.Awb)).
						WithDetail("awb", ob.Awb)
				}
				return apierr.Internal(err)
			}
		}

		rows, err := q.ListTransferItems(ctx, created.ID)
		if err != nil {
			return apierr.Internal(err)
		}
		transfer, items = &created, rows

		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "cod.custody.declared", ResourceType: "cod_custody_transfer",
			ResourceID: &created.ID, ResourcePublicID: created.PublicID,
			Metadata: map[string]any{
				"transferCode": code, "declaredMinor": declared,
				"shipmentCount": len(locked),
				"from":          in.FromType, "to": in.ToType,
			},
		}))
	})
	return transfer, items, err
}

// AcceptTransferRequest is the receiver's side.
type AcceptTransferRequest struct {
	TransferID string
	// What the receiver actually counted. A difference from the declaration is
	// a variance and must be explained.
	AcceptedMinor  int64
	VarianceReason string
}

// AcceptTransfer completes a hand-off and moves the receivable between parties.
//
// The variance handling is the substance here. If the receiver counts less than
// was declared, the shortfall does not vanish: it moves to COD Shortage
// Recoverable against the party that declared it, so somebody remains
// accountable for it until an adjustment resolves it.
func (s *Service) AcceptTransfer(
	ctx context.Context, p *tenant.Principal, in AcceptTransferRequest,
) (*dbgen.CodCustodyTransfer, error) {
	var out *dbgen.CodCustodyTransfer

	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		header, err := q.GetTransferByPublicID(ctx, dbgen.GetTransferByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: in.TransferID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Custody transfer")
		}
		locked, err := q.LockTransfer(ctx, dbgen.LockTransferParams{
			OrganizationID: p.OrganizationID, ID: header.ID,
		})
		if err != nil {
			return apierr.Internal(err)
		}
		if locked.Status != TransferDeclared {
			return apierr.Conflict(CodeTransferClosed,
				fmt.Sprintf("Transfer %s is %s and can no longer be accepted.",
					locked.TransferCode, locked.Status))
		}

		variance := in.AcceptedMinor - locked.DeclaredMinor
		if variance != 0 && len([]rune(in.VarianceReason)) < 3 {
			return apierr.Validation(
				"The counted amount differs from the declaration, so a reason is required.",
				map[string]any{
					"declaredMinor": locked.DeclaredMinor,
					"acceptedMinor": in.AcceptedMinor,
					"varianceMinor": variance,
				})
		}

		items, err := q.ListTransferItems(ctx, locked.ID)
		if err != nil {
			return apierr.Internal(err)
		}

		fromAccount, fromParty, err := receivableAccountFor(locked.FromType)
		if err != nil {
			return err
		}
		toAccount, toParty, err := receivableAccountFor(locked.ToType)
		if err != nil {
			return err
		}
		toStatus, err := statusForCustodian(locked.ToType)
		if err != nil {
			return err
		}
		fromStatus, err := statusForCustodian(locked.FromType)
		if err != nil {
			return err
		}

		// The accepted amount moves between the two parties' accounts. Any
		// shortfall stays chargeable to the giver rather than evaporating.
		legs := []ledger.Leg{
			{
				AccountCode: toAccount, Debit: in.AcceptedMinor,
				PartyType: toParty, PartyID: locked.ToID,
				Memo: fmt.Sprintf("COD accepted from %s", locked.FromType),
			},
			{
				AccountCode: fromAccount, Credit: locked.DeclaredMinor,
				PartyType: fromParty, PartyID: locked.FromID,
				Memo: fmt.Sprintf("COD handed to %s", locked.ToType),
			},
		}
		switch {
		case variance < 0:
			// Short: the difference becomes recoverable from the giver.
			legs = append(legs, ledger.Leg{
				AccountCode: ledger.AcctCODShortageRecoverable, Debit: -variance,
				PartyType: fromParty, PartyID: locked.FromID,
				Memo: "Shortfall on hand-in: " + in.VarianceReason,
			})
		case variance > 0:
			// Over: the excess is owed back rather than quietly kept.
			legs = append(legs, ledger.Leg{
				AccountCode: ledger.AcctCODExcessPayable, Credit: variance,
				Memo: "Excess on hand-in: " + in.VarianceReason,
			})
		}

		posting, err := s.ledger.Post(ctx, tx, p, ledger.Posting{
			SourceType:     ledger.SourceCOD,
			SourceID:       &locked.ID,
			SourcePublicID: locked.PublicID,
			Purpose:        "CUSTODY_TRANSFER",
			Description: fmt.Sprintf("COD custody %s → %s (%s)",
				locked.FromType, locked.ToType, locked.TransferCode),
			Currency:    locked.Currency,
			PostingDate: time.Now().UTC(),
			Legs:        legs,
			Metadata: map[string]any{
				"transferCode": locked.TransferCode, "varianceMinor": variance,
			},
		})
		if err != nil {
			return err
		}

		accepted, err := q.AcceptCustodyTransfer(ctx, dbgen.AcceptCustodyTransferParams{
			OrganizationID: p.OrganizationID, ID: locked.ID,
			AcceptedMinor: &in.AcceptedMinor, AcceptedBy: &p.UserID,
			VarianceMinor:        variance,
			VarianceReason:       ops.Optional(in.VarianceReason),
			JournalTransactionID: &posting.Transaction.ID,
		})
		if err != nil {
			if ops.IsNoRows(err) {
				return apierr.Conflict(CodeTransferClosed,
					"This transfer was accepted by another request.")
			}
			return apierr.Internal(err)
		}

		// Move each obligation's custody pointer.
		for _, item := range items {
			if _, err := q.MoveObligationCustody(ctx, dbgen.MoveObligationCustodyParams{
				OrganizationID: p.OrganizationID, ID: item.ObligationID,
				FromStatus: fromStatus, ToStatus: toStatus,
				CustodianType: ops.Optional(locked.ToType),
				CustodianID:   &locked.ToID,
			}); err != nil {
				if ops.IsNoRows(err) {
					return apierr.Conflict(CodeInvalidState,
						fmt.Sprintf("COD for %s changed state during the hand-off.", item.Awb))
				}
				return apierr.Internal(err)
			}
		}

		out = &accepted
		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "cod.custody.accepted", ResourceType: "cod_custody_transfer",
			ResourceID: &locked.ID, ResourcePublicID: locked.PublicID,
			Reason: in.VarianceReason,
			Metadata: map[string]any{
				"transferCode":  locked.TransferCode,
				"declaredMinor": locked.DeclaredMinor,
				"acceptedMinor": in.AcceptedMinor,
				"varianceMinor": variance,
			},
		}))
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Remittance
// ---------------------------------------------------------------------------

// RemitRequest sends money on toward head office or the consignor.
type RemitRequest struct {
	FromType        string
	FromID          int64
	BeneficiaryType string
	BeneficiaryID   *int64
	ObligationIDs   []string
	PaymentMode     string
	Reference       string
	PaidOn          time.Time
}

// CreateRemittance records money leaving a party toward head office.
//
// The obligation is not marked REMITTED until the remittance is confirmed:
// initiating a bank transfer is not the same as the money arriving.
func (s *Service) CreateRemittance(
	ctx context.Context, p *tenant.Principal, in RemitRequest,
) (*dbgen.CodRemittance, error) {
	if len(in.ObligationIDs) == 0 {
		return nil, apierr.Validation("A remittance needs at least one COD shipment.", nil)
	}
	if in.PaidOn.IsZero() {
		in.PaidOn = time.Now().UTC()
	}
	expectedStatus, err := statusForCustodian(in.FromType)
	if err != nil {
		return nil, err
	}

	var out *dbgen.CodRemittance
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		ids, err := s.resolveObligationIDs(ctx, q, p.OrganizationID, in.ObligationIDs)
		if err != nil {
			return err
		}
		locked, err := q.LockObligationsByIDs(ctx, dbgen.LockObligationsByIDsParams{
			OrganizationID: p.OrganizationID, Ids: ids,
		})
		if err != nil {
			return apierr.Internal(err)
		}

		var total int64
		currency := ""
		for _, ob := range locked {
			if ob.Status != expectedStatus && ob.Status != StatusReconciled {
				return apierr.Conflict(CodeInvalidState,
					fmt.Sprintf("COD for %s is %s and cannot be remitted.", ob.Awb, ob.Status))
			}
			if currency == "" {
				currency = ob.Currency
			}
			total += ob.CollectedMinor - ob.RemittedMinor
		}
		if total <= 0 {
			return apierr.Conflict(CodeNothingToRemit,
				"These shipments have nothing left to remit.")
		}

		code, err := s.allocateCode(ctx, q, p.OrganizationID, "CDM")
		if err != nil {
			return err
		}

		created, err := q.CreateCODRemittance(ctx, dbgen.CreateCODRemittanceParams{
			PublicID:       publicid.New("cdm"),
			OrganizationID: p.OrganizationID,
			RemittanceCode: code,
			FromType:       in.FromType, FromID: in.FromID,
			BeneficiaryType: in.BeneficiaryType, BeneficiaryID: in.BeneficiaryID,
			AmountMinor: total, Currency: currency,
			ShipmentCount: int32(len(locked)),
			PaymentMode:   in.PaymentMode,
			Reference:     ops.Optional(in.Reference),
			PaidOn:        in.PaidOn,
			CreatedBy:     &p.UserID,
			RequestID:     ops.Optional(httpx.RequestID(ctx)),
		})
		if err != nil {
			return apierr.Internal(err)
		}
		for _, ob := range locked {
			if _, err := q.AddRemittanceItem(ctx, dbgen.AddRemittanceItemParams{
				RemittanceID: created.ID, ObligationID: ob.ID,
				AmountMinor: ob.CollectedMinor - ob.RemittedMinor,
			}); err != nil {
				return apierr.Internal(err)
			}
		}
		out = &created
		return nil
	})
	return out, err
}

// ConfirmRemittance records that the money arrived and posts it.
func (s *Service) ConfirmRemittance(
	ctx context.Context, p *tenant.Principal, publicID string,
) (result *dbgen.CodRemittance, metricErr error) {
	// Counted here, at the one place the outcome is decided, so the
	// metric cannot drift from what actually happened. A named error
	// result makes this correct on every return path rather than only
	// the ones somebody remembered to instrument.
	defer func() {
		if metricErr != nil {
			s.metrics.RecordBusiness("cod_remittance", "failed")
			return
		}
		s.metrics.RecordBusiness("cod_remittance", "confirmed")
	}()

	var out *dbgen.CodRemittance

	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		header, err := q.GetRemittanceByPublicID(ctx, dbgen.GetRemittanceByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: publicID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "COD remittance")
		}
		locked, err := q.LockRemittance(ctx, dbgen.LockRemittanceParams{
			OrganizationID: p.OrganizationID, ID: header.ID,
		})
		if err != nil {
			return apierr.Internal(err)
		}
		if locked.Status != "INITIATED" {
			return apierr.Conflict(CodeInvalidState,
				fmt.Sprintf("Remittance %s is %s.", locked.RemittanceCode, locked.Status))
		}

		fromAccount, fromParty, err := receivableAccountFor(locked.FromType)
		if err != nil {
			return err
		}

		// Money reaching head office clears the holder's receivable.
		// Money reaching the consignor clears the liability opened at
		// collection; the two are different journals because they close
		// different obligations.
		var legs []ledger.Leg
		switch locked.BeneficiaryType {
		case "HEAD_OFFICE":
			legs = []ledger.Leg{
				{AccountCode: ledger.AcctCash, Debit: locked.AmountMinor,
					Memo: "COD banked at head office"},
				{AccountCode: fromAccount, Credit: locked.AmountMinor,
					PartyType: fromParty, PartyID: locked.FromID,
					Memo: "COD remitted to head office"},
			}
		case "CONSIGNOR", "CUSTOMER":
			legs = []ledger.Leg{
				{AccountCode: ledger.AcctCODPayableConsignor, Debit: locked.AmountMinor,
					CustomerID: locked.BeneficiaryID,
					Memo:       "COD paid to the consignor"},
				{AccountCode: ledger.AcctCash, Credit: locked.AmountMinor,
					Memo: "Paid out to the consignor"},
			}
		default:
			return apierr.Validation("Unknown remittance beneficiary.",
				map[string]any{"beneficiaryType": locked.BeneficiaryType})
		}

		posting, err := s.ledger.Post(ctx, tx, p, ledger.Posting{
			SourceType:     ledger.SourceCOD,
			SourceID:       &locked.ID,
			SourcePublicID: locked.PublicID,
			Purpose:        "REMITTANCE",
			Description: fmt.Sprintf("COD remittance %s to %s",
				locked.RemittanceCode, locked.BeneficiaryType),
			Currency: locked.Currency, PostingDate: locked.PaidOn,
			Legs:     legs,
			Metadata: map[string]any{"remittanceCode": locked.RemittanceCode},
		})
		if err != nil {
			return err
		}

		confirmed, err := q.ConfirmCODRemittance(ctx, dbgen.ConfirmCODRemittanceParams{
			OrganizationID: p.OrganizationID, ID: locked.ID,
			ConfirmedBy:          &p.UserID,
			JournalTransactionID: &posting.Transaction.ID,
		})
		if err != nil {
			if ops.IsNoRows(err) {
				return apierr.Conflict(CodeInvalidState,
					"This remittance was confirmed by another request.")
			}
			return apierr.Internal(err)
		}

		items, err := q.ListRemittanceItems(ctx, locked.ID)
		if err != nil {
			return apierr.Internal(err)
		}
		for _, item := range items {
			if _, err := q.MarkObligationRemitted(ctx, dbgen.MarkObligationRemittedParams{
				OrganizationID: p.OrganizationID, ID: item.ObligationID,
				AmountMinor: item.AmountMinor,
			}); err != nil && !ops.IsNoRows(err) {
				return apierr.Internal(err)
			}
		}

		out = &confirmed
		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "cod.remittance.confirmed", ResourceType: "cod_remittance",
			ResourceID: &locked.ID, ResourcePublicID: locked.PublicID,
			Metadata: map[string]any{
				"remittanceCode": locked.RemittanceCode,
				"amountMinor":    locked.AmountMinor,
				"beneficiary":    locked.BeneficiaryType,
			},
		}))
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Adjustments — maker/checker
// ---------------------------------------------------------------------------

// AdjustmentRequest raises a correction for approval.
type AdjustmentRequest struct {
	ObligationID     string
	ReconciliationID string
	Type             string
	AmountMinor      int64
	LiablePartyType  string
	LiablePartyID    *int64
	Reason           string
}

// RequestAdjustment raises a COD correction. It does not move money: an
// adjustment only posts once a second person approves it.
func (s *Service) RequestAdjustment(
	ctx context.Context, p *tenant.Principal, in AdjustmentRequest,
) (*dbgen.CodAdjustment, error) {
	if len([]rune(in.Reason)) < 10 {
		return nil, apierr.Validation("A COD adjustment requires a substantive reason.",
			map[string]any{"reason": "at least 10 characters"})
	}
	if in.AmountMinor == 0 {
		return nil, apierr.Validation("An adjustment cannot be for zero.", nil)
	}

	var out *dbgen.CodAdjustment
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		var obligationID *int64
		currency := p.OrganizationCurrency
		if in.ObligationID != "" {
			ob, err := q.GetObligationByPublicID(ctx, dbgen.GetObligationByPublicIDParams{
				OrganizationID: p.OrganizationID, PublicID: in.ObligationID,
			})
			if err != nil {
				return ops.NotFoundOr(err, "COD obligation")
			}
			obligationID, currency = &ob.ID, ob.Currency
		}

		created, err := q.CreateCODAdjustment(ctx, dbgen.CreateCODAdjustmentParams{
			PublicID:        publicid.New("cda"),
			OrganizationID:  p.OrganizationID,
			ObligationID:    obligationID,
			AdjustmentType:  in.Type,
			AmountMinor:     in.AmountMinor,
			Currency:        currency,
			LiablePartyType: ops.Optional(in.LiablePartyType),
			LiablePartyID:   in.LiablePartyID,
			Reason:          in.Reason,
			RequestedBy:     p.UserID,
			RequestID:       ops.Optional(httpx.RequestID(ctx)),
		})
		if err != nil {
			return apierr.Internal(err)
		}
		out = &created

		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "cod.adjustment.requested", ResourceType: "cod_adjustment",
			ResourceID: &created.ID, ResourcePublicID: created.PublicID,
			Reason: in.Reason,
			Metadata: map[string]any{
				"type": in.Type, "amountMinor": in.AmountMinor,
			},
		}))
	})
	return out, err
}

// ApproveAdjustment approves and posts a COD correction.
//
// Maker/checker is enforced three times over: the service compares the ids, the
// UPDATE carries `requested_by <> approved_by`, and a CHECK constraint refuses
// the row outright. A single person cannot write off their own shortage however
// they reach the database.
func (s *Service) ApproveAdjustment(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*dbgen.CodAdjustment, error) {
	var out *dbgen.CodAdjustment

	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		header, err := q.GetAdjustmentByPublicID(ctx, dbgen.GetAdjustmentByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: publicID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "COD adjustment")
		}
		if header.RequestedBy == p.UserID {
			return apierr.Forbidden(
				"A COD adjustment must be approved by someone other than the person who raised it.")
		}

		approver := p.UserID
		approved, err := q.ApproveCODAdjustment(ctx, dbgen.ApproveCODAdjustmentParams{
			OrganizationID: p.OrganizationID, ID: header.ID, ApprovedBy: &approver,
		})
		if err != nil {
			if ops.IsNoRows(err) {
				return apierr.Conflict(CodeNotApproved,
					"This adjustment is no longer pending approval, or you raised it yourself.")
			}
			return apierr.Internal(err)
		}

		legs, err := s.adjustmentLegs(approved)
		if err != nil {
			return err
		}
		posting, err := s.ledger.Post(ctx, tx, p, ledger.Posting{
			SourceType:     ledger.SourceAdjustment,
			SourceID:       &approved.ID,
			SourcePublicID: approved.PublicID,
			Purpose:        "COD_" + approved.AdjustmentType,
			Description:    fmt.Sprintf("COD adjustment: %s", approved.AdjustmentType),
			Reason:         approved.Reason,
			Currency:       approved.Currency,
			PostingDate:    time.Now().UTC(),
			Legs:           legs,
		})
		if err != nil {
			return err
		}

		posted, err := q.MarkAdjustmentPosted(ctx, dbgen.MarkAdjustmentPostedParams{
			OrganizationID: p.OrganizationID, ID: approved.ID,
			JournalTransactionID: &posting.Transaction.ID,
		})
		if err != nil {
			return apierr.Internal(err)
		}
		out = &posted

		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "cod.adjustment.approved", ResourceType: "cod_adjustment",
			ResourceID: &approved.ID, ResourcePublicID: approved.PublicID,
			Reason: approved.Reason,
			Metadata: map[string]any{
				"type": approved.AdjustmentType, "amountMinor": approved.AmountMinor,
				"requestedBy": header.RequestedBy, "approvedBy": p.UserID,
			},
		}))
	})
	return out, err
}

// adjustmentLegs maps an adjustment type onto its accounting.
func (s *Service) adjustmentLegs(a dbgen.CodAdjustment) ([]ledger.Leg, error) {
	amount := a.AmountMinor
	if amount < 0 {
		amount = -amount
	}

	partyType := ledger.PartyFranchise
	if a.LiablePartyType != nil {
		switch *a.LiablePartyType {
		case PartyAgent:
			partyType = ledger.PartyAgent
		case PartyUnit:
			partyType = ledger.PartyOperatingUnit
		}
	}
	var partyID int64
	if a.LiablePartyID != nil {
		partyID = *a.LiablePartyID
	}

	switch a.AdjustmentType {
	case AdjShortageWriteOff, AdjWaiver:
		// The network absorbs the loss: the recoverable becomes an expense.
		return []ledger.Leg{
			{AccountCode: ledger.AcctCODShortageWrittenOff, Debit: amount,
				Memo: "Shortage written off"},
			{AccountCode: ledger.AcctCODShortageRecoverable, Credit: amount,
				PartyType: partyType, PartyID: partyID,
				Memo: "Recoverable cleared"},
		}, nil

	case AdjShortageRecovery:
		// The liable party pays it back.
		return []ledger.Leg{
			{AccountCode: ledger.AcctCash, Debit: amount, Memo: "Shortage recovered"},
			{AccountCode: ledger.AcctCODShortageRecoverable, Credit: amount,
				PartyType: partyType, PartyID: partyID,
				Memo: "Recoverable settled"},
		}, nil

	case AdjExcessRefund:
		return []ledger.Leg{
			{AccountCode: ledger.AcctCODExcessPayable, Debit: amount,
				Memo: "Excess refunded"},
			{AccountCode: ledger.AcctCash, Credit: amount, Memo: "Paid out"},
		}, nil

	case AdjExcessRetained:
		// Unclaimed over-collection becomes income rather than sitting as a
		// liability forever.
		return []ledger.Leg{
			{AccountCode: ledger.AcctCODExcessPayable, Debit: amount,
				Memo: "Excess retained"},
			{AccountCode: ledger.AcctOtherRevenue, Credit: amount,
				Memo: "Unclaimed COD excess"},
		}, nil

	case AdjCorrection, AdjDisputeResolution:
		// A correction moves value between the recoverable and the holder's
		// receivable; direction follows the sign the maker requested.
		if a.AmountMinor > 0 {
			return []ledger.Leg{
				{AccountCode: ledger.AcctCODShortageRecoverable, Debit: amount,
					PartyType: partyType, PartyID: partyID, Memo: a.Reason},
				{AccountCode: ledger.AcctCODReceivableFranchise, Credit: amount,
					PartyType: partyType, PartyID: partyID, Memo: a.Reason},
			}, nil
		}
		return []ledger.Leg{
			{AccountCode: ledger.AcctCODReceivableFranchise, Debit: amount,
				PartyType: partyType, PartyID: partyID, Memo: a.Reason},
			{AccountCode: ledger.AcctCODShortageRecoverable, Credit: amount,
				PartyType: partyType, PartyID: partyID, Memo: a.Reason},
		}, nil
	}

	return nil, apierr.Validation("Unknown COD adjustment type.",
		map[string]any{"adjustmentType": a.AdjustmentType})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// resolveObligationIDs turns public ids into internal ids, refusing any that do
// not belong to this tenant — which is where cross-tenant access would
// otherwise leak in a batch operation.
func (s *Service) resolveObligationIDs(
	ctx context.Context, q *dbgen.Queries, orgID int64, publicIDs []string,
) ([]int64, error) {
	ids := make([]int64, 0, len(publicIDs))
	for _, pid := range publicIDs {
		ob, err := q.GetObligationByPublicID(ctx, dbgen.GetObligationByPublicIDParams{
			OrganizationID: orgID, PublicID: pid,
		})
		if err != nil {
			if ops.IsNoRows(err) {
				return nil, apierr.NotFound("COD obligation")
			}
			return nil, apierr.Internal(err)
		}
		ids = append(ids, ob.ID)
	}
	return ids, nil
}

// allocateCode produces the next human-readable code for a COD document.
func (s *Service) allocateCode(
	ctx context.Context, q *dbgen.Queries, orgID int64, kind string,
) (string, error) {
	scope := time.Now().UTC().Format("200601")
	next, err := q.AllocateCODSequence(ctx, dbgen.AllocateCODSequenceParams{
		OrganizationID: orgID, Kind: kind, ScopeKey: scope,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return "", apierr.Conflict(apierr.CodeConflict,
				fmt.Sprintf("The %s sequence is exhausted for this month.", kind))
		}
		return "", apierr.Internal(err)
	}
	return fmt.Sprintf("%s-%s-%06d", kind, scope, next), nil
}
