package commission

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

// RuleInput describes a new commission rule.
//
// Scope is expressed with public identifiers rather than internal ids, so a
// client never sees or supplies a primary key (§11), and each is resolved under
// the authenticated tenant — which is where a cross-tenant reference would
// otherwise slip in.
type RuleInput struct {
	SchemeCode        string
	Code              string
	Name              string
	CommissionType    string
	RecipientRole     string
	FranchisePublicID string
	FranchiseCategory string
	ServiceCode       string
	CustomerCategory  string
	PaymentMode       string
	Priority          int32
	Description       string
}

// CreateRule adds a rule to a scheme.
func (s *Service) CreateRule(
	ctx context.Context, p *tenant.Principal, in RuleInput,
) (*dbgen.CommissionRule, error) {
	if in.Code == "" || in.Name == "" || in.CommissionType == "" || in.RecipientRole == "" {
		return nil, apierr.Validation(
			"A rule needs a code, a name, a commission type and a recipient role.", nil)
	}

	var out *dbgen.CommissionRule
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		schemeCode := in.SchemeCode
		if schemeCode == "" {
			schemeCode = "STANDARD"
		}
		scheme, err := q.GetSchemeByCode(ctx, dbgen.GetSchemeByCodeParams{
			OrganizationID: p.OrganizationID, Code: schemeCode,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Commission scheme")
		}

		franchiseID, err := s.resolveFranchise(ctx, q, p, in.FranchisePublicID)
		if err != nil {
			return err
		}
		serviceID, err := s.resolveService(ctx, q, p, in.ServiceCode)
		if err != nil {
			return err
		}

		created, err := q.CreateCommissionRule(ctx, dbgen.CreateCommissionRuleParams{
			PublicID:          publicid.New("crl"),
			OrganizationID:    p.OrganizationID,
			SchemeID:          scheme.ID,
			Code:              in.Code,
			Name:              in.Name,
			CommissionType:    in.CommissionType,
			RecipientRole:     in.RecipientRole,
			FranchiseID:       franchiseID,
			FranchiseCategory: ops.Optional(in.FranchiseCategory),
			ServiceID:         serviceID,
			CustomerCategory:  ops.Optional(in.CustomerCategory),
			PaymentMode:       ops.Optional(in.PaymentMode),
			Priority:          in.Priority,
			Status:            "ACTIVE",
			Description:       ops.Optional(in.Description),
			CreatedBy:         &p.UserID,
		})
		if err != nil {
			if ops.IsUnique(err, "commission_rules_code_idx") {
				return apierr.Conflict(apierr.CodeConflict,
					fmt.Sprintf("Rule code %s is already in use.", in.Code))
			}
			return apierr.Internal(err)
		}
		out = &created

		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "commission.rule.created", ResourceType: "commission_rule",
			ResourceID: &created.ID, ResourcePublicID: created.PublicID,
			Metadata: map[string]any{
				"code": in.Code, "commissionType": in.CommissionType,
				"specificity": created.Specificity,
			},
		}))
	})
	return out, err
}

// VersionInput describes a new effective-dated version of a rule.
type VersionInput struct {
	Method           string
	FixedAmountMinor *int64
	RateBp           *int32
	Basis            string
	Slabs            []Slab
	MinAmountMinor   *int64
	MaxAmountMinor   *int64
	EffectiveFrom    time.Time
	Notes            string
}

// CreateVersion adds a version and supersedes the one it replaces.
//
// Changing a rate is always a new version, never an edit: the guard trigger in
// migration 0021 refuses to alter a version that has already produced a
// calculation, so history cannot move under a franchise's feet.
func (s *Service) CreateVersion(
	ctx context.Context, p *tenant.Principal, rulePublicID string, in VersionInput,
) (*dbgen.CommissionRuleVersion, error) {
	if in.EffectiveFrom.IsZero() {
		return nil, apierr.Validation("A version needs an effective date.", nil)
	}
	if in.Method == MethodSlab {
		if err := ValidateSlabs(in.Slabs); err != nil {
			return nil, err
		}
	}

	var out *dbgen.CommissionRuleVersion
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		rule, err := q.GetRuleByPublicID(ctx, dbgen.GetRuleByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: rulePublicID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Commission rule")
		}

		next, err := q.NextRuleVersionNumber(ctx, rule.ID)
		if err != nil {
			return apierr.Internal(err)
		}

		var slabs []byte
		if len(in.Slabs) > 0 {
			b, mErr := json.Marshal(in.Slabs)
			if mErr != nil {
				return apierr.Internal(mErr)
			}
			slabs = b
		}

		created, err := q.CreateRuleVersion(ctx, dbgen.CreateRuleVersionParams{
			PublicID:          publicid.New("crv"),
			OrganizationID:    p.OrganizationID,
			RuleID:            rule.ID,
			VersionNo:         int32(next),
			CalculationMethod: in.Method,
			Currency:          p.OrganizationCurrency,
			FixedAmountMinor:  in.FixedAmountMinor,
			RateBp:            in.RateBp,
			Basis:             ops.Optional(in.Basis),
			Slabs:             slabs,
			MinAmountMinor:    in.MinAmountMinor,
			MaxAmountMinor:    in.MaxAmountMinor,
			EffectiveFrom:     in.EffectiveFrom,
			Status:            "ACTIVE",
			Notes:             ops.Optional(in.Notes),
			CreatedBy:         &p.UserID,
		})
		if err != nil {
			return apierr.Internal(err)
		}

		// Close the predecessor so two versions never both claim the same day.
		if err := q.SupersedePriorVersions(ctx, dbgen.SupersedePriorVersionsParams{
			RuleID: rule.ID, NewVersionID: created.ID, EffectiveFrom: in.EffectiveFrom,
		}); err != nil {
			return apierr.Internal(err)
		}

		out = &created
		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "commission.rule.versioned", ResourceType: "commission_rule",
			ResourceID: &rule.ID, ResourcePublicID: rule.PublicID,
			Metadata: map[string]any{
				"ruleCode": rule.Code, "versionNo": created.VersionNo,
				"method": in.Method, "effectiveFrom": in.EffectiveFrom.Format("2006-01-02"),
			},
		}))
	})
	return out, err
}

// GetRule returns a rule with its version history.
func (s *Service) GetRule(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*dbgen.GetRuleByPublicIDRow, []dbgen.CommissionRuleVersion, error) {
	rule, err := s.q.GetRuleByPublicID(ctx, dbgen.GetRuleByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, nil, ops.NotFoundOr(err, "Commission rule")
	}
	versions, err := s.q.ListRuleVersions(ctx, rule.ID)
	if err != nil {
		return nil, nil, apierr.Internal(err)
	}
	return &rule, versions, nil
}

// ListRules returns the rule catalogue.
func (s *Service) ListRules(
	ctx context.Context, p *tenant.Principal,
	commissionType, status string, franchiseID *int64, limit, offset int32,
) ([]dbgen.ListCommissionRulesRow, int64, error) {
	rows, err := s.q.ListCommissionRules(ctx, dbgen.ListCommissionRulesParams{
		OrganizationID: p.OrganizationID,
		CommissionType: ops.Optional(commissionType),
		Status:         ops.Optional(status),
		FranchiseID:    franchiseID,
		Limit:          limit, Offset: offset,
	})
	if err != nil {
		return nil, 0, apierr.Internal(err)
	}
	total, err := s.q.CountCommissionRules(ctx, dbgen.CountCommissionRulesParams{
		OrganizationID: p.OrganizationID,
		CommissionType: ops.Optional(commissionType),
		Status:         ops.Optional(status),
		FranchiseID:    franchiseID,
	})
	if err != nil {
		return nil, 0, apierr.Internal(err)
	}
	return rows, total, nil
}

// ListCalculations returns computed commission.
func (s *Service) ListCalculations(
	ctx context.Context, p *tenant.Principal,
	commissionType, status string, franchiseID, cursor *int64, limit int32,
) ([]dbgen.ListCommissionCalculationsRow, error) {
	rows, err := s.q.ListCommissionCalculations(ctx, dbgen.ListCommissionCalculationsParams{
		OrganizationID: p.OrganizationID,
		CommissionType: ops.Optional(commissionType),
		Status:         ops.Optional(status),
		FranchiseID:    franchiseID,
		CursorID:       cursor,
		Limit:          limit,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return rows, nil
}

// GetCalculation returns one calculation with its trace.
func (s *Service) GetCalculation(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*dbgen.GetCalculationByPublicIDRow, error) {
	calc, err := s.q.GetCalculationByPublicID(ctx, dbgen.GetCalculationByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Commission calculation")
	}
	return &calc, nil
}

// ResolveFranchiseID turns a franchise public id into an internal id, under
// this tenant. Exported because the transport needs it to build simulation
// facts without reaching into the database itself.
func (s *Service) ResolveFranchiseID(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*int64, error) {
	return s.resolveFranchise(ctx, s.q, p, publicID)
}

func (s *Service) resolveFranchise(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, publicID string,
) (*int64, error) {
	if publicID == "" {
		return nil, nil
	}
	f, err := q.GetFranchiseByPublicID(ctx, dbgen.GetFranchiseByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Franchise")
	}
	return &f.ID, nil
}

func (s *Service) resolveService(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, code string,
) (*int64, error) {
	if code == "" {
		return nil, nil
	}
	svc, err := q.GetCourierServiceByCode(ctx, dbgen.GetCourierServiceByCodeParams{
		OrganizationID: p.OrganizationID, Code: code,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Courier service")
	}
	return &svc.ID, nil
}
