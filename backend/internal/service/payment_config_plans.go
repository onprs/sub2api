package service

import (
	"context"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionplan"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	validityUnitDay  = "day"
	validityUnitDays = "days"
)

// normalizePlanCurrency validates and normalizes the display-only currency label.
// Empty means "no label" and is kept as-is so existing plans stay unchanged.
func normalizePlanCurrency(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	currency, err := payment.NormalizePaymentCurrency(raw)
	if err != nil {
		return "", infraerrors.BadRequest("PLAN_CURRENCY_INVALID", "currency must be a 3-letter ISO currency code")
	}
	return currency, nil
}

// validatePlanRequired checks that all required fields for a plan are provided.
// The optional numeric tail is kept for historical tests: first three values are
// rolling quota limits, the fourth value is renewal_discount_percent.
func validatePlanRequired(name string, groupID int64, price float64, validityDays int, validityUnit string, originalPrice *float64, numericOptions ...*float64) error {
	if strings.TrimSpace(name) == "" {
		return infraerrors.BadRequest("PLAN_NAME_REQUIRED", "plan name is required")
	}
	if groupID <= 0 {
		return infraerrors.BadRequest("PLAN_GROUP_REQUIRED", "group is required")
	}
	if price <= 0 {
		return infraerrors.BadRequest("PLAN_PRICE_INVALID", "price must be > 0")
	}
	if validityDays <= 0 {
		return infraerrors.BadRequest("PLAN_VALIDITY_REQUIRED", "validity days must be > 0")
	}
	if normalizePlanValidityUnit(validityUnit) == "" {
		return infraerrors.BadRequest("PLAN_VALIDITY_UNIT_REQUIRED", "validity unit is required")
	}
	if originalPrice != nil && *originalPrice < 0 {
		return infraerrors.BadRequest("PLAN_ORIGINAL_PRICE_INVALID", "original price must be >= 0")
	}
	for i, limit := range numericOptions {
		if i >= 3 {
			break
		}
		name := "plan_limit_usd"
		switch i {
		case 0:
			name = "five_hour_limit_usd"
		case 1:
			name = "seven_day_limit_usd"
		case 2:
			name = "thirty_day_limit_usd"
		}
		if err := validatePlanRollingLimit(name, limit); err != nil {
			return err
		}
	}
	if len(numericOptions) >= 4 {
		if err := validatePlanRenewalDiscountPercent(numericOptions[3], price); err != nil {
			return err
		}
	}
	return nil
}

func validatePlanRollingLimit(name string, value *float64) error {
	if value != nil && *value < 0 {
		return infraerrors.BadRequest("PLAN_LIMIT_INVALID", name+" must be >= 0")
	}
	return nil
}

func validatePlanStock(value *int) error {
	if value != nil && *value < 0 {
		return infraerrors.BadRequest("PLAN_STOCK_INVALID", "stock must be >= 0")
	}
	return nil
}

func SubscriptionPlanSoldOut(plan *dbent.SubscriptionPlan) bool {
	return plan != nil && plan.Stock != nil && *plan.Stock == 0
}

// DecSubscriptionPlanStock atomically decrements a plan's finite stock by one
// within the given client (which may be a transaction client). It returns:
//   - (true, nil) when stock was decremented.
//   - (false, nil) when the plan does not exist or has unlimited (NULL) stock
//     — caller treats this as "no stock to decrement".
//   - (false, err) when stock is finite but already 0 (sold out) or on a
//     database error.
func DecSubscriptionPlanStock(ctx context.Context, client *dbent.Client, planID int64) (bool, error) {
	plan, err := client.SubscriptionPlan.Get(ctx, planID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get subscription plan for stock: %w", err)
	}
	if plan.Stock == nil {
		// Unlimited stock — nothing to decrement.
		return false, nil
	}
	if *plan.Stock <= 0 {
		return false, infraerrors.Conflict("PLAN_SOLD_OUT", "subscription plan is sold out")
	}
	n, err := client.SubscriptionPlan.Update().
		Where(
			subscriptionplan.IDEQ(planID),
			subscriptionplan.StockNotNil(),
			subscriptionplan.StockGT(0),
		).
		AddStock(-1).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("decrement subscription plan stock: %w", err)
	}
	if n == 0 {
		return false, infraerrors.Conflict("PLAN_SOLD_OUT", "subscription plan is sold out")
	}
	return true, nil
}

// IncSubscriptionPlanStock atomically restores one unit of stock for a plan
// with finite (NotNil) stock. No-op for unlimited-stock plans. It returns
// (true, nil) when stock was restored, (false, nil) when the plan does not
// exist or has unlimited stock.
func IncSubscriptionPlanStock(ctx context.Context, client *dbent.Client, planID int64) (bool, error) {
	plan, err := client.SubscriptionPlan.Get(ctx, planID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get subscription plan for stock: %w", err)
	}
	if plan.Stock == nil {
		return false, nil
	}
	_, err = client.SubscriptionPlan.Update().
		Where(subscriptionplan.IDEQ(planID), subscriptionplan.StockNotNil()).
		AddStock(1).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("increment subscription plan stock: %w", err)
	}
	return true, nil
}

func validatePlanRenewalDiscountPercentRange(value *float64) error {
	if value == nil {
		return nil
	}
	if *value < 0 || *value >= 100 {
		return infraerrors.BadRequest("PLAN_RENEWAL_DISCOUNT_INVALID", "renewal_discount_percent must be >= 0 and < 100")
	}
	return nil
}

func validatePlanRenewalDiscountPercent(value *float64, price float64) error {
	if err := validatePlanRenewalDiscountPercentRange(value); err != nil {
		return err
	}
	if value == nil {
		return nil
	}
	if *value == 0 {
		return nil
	}
	if calculateRenewalDiscountedPrice(price, *value) <= 0 {
		return infraerrors.BadRequest("PLAN_RENEWAL_DISCOUNT_INVALID", "renewal discount makes the renewal price too low")
	}
	return nil
}

func normalizePlanValidityUnit(unit string) string {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case validityUnitDay, validityUnitDays:
		return validityUnitDays
	case validityUnitWeek, validityUnitWeeks:
		return validityUnitWeeks
	case validityUnitMonth, validityUnitMonths:
		return validityUnitMonths
	default:
		return ""
	}
}

// validatePlanPatch validates only the non-nil fields in a patch update.
func validatePlanPatch(req UpdatePlanRequest) error {
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		return infraerrors.BadRequest("PLAN_NAME_REQUIRED", "plan name is required")
	}
	if req.GroupID != nil && *req.GroupID <= 0 {
		return infraerrors.BadRequest("PLAN_GROUP_REQUIRED", "group is required")
	}
	if req.Price != nil && *req.Price <= 0 {
		return infraerrors.BadRequest("PLAN_PRICE_INVALID", "price must be > 0")
	}
	if req.ValidityDays != nil && *req.ValidityDays <= 0 {
		return infraerrors.BadRequest("PLAN_VALIDITY_REQUIRED", "validity days must be > 0")
	}
	if req.ValidityUnit != nil && normalizePlanValidityUnit(*req.ValidityUnit) == "" {
		return infraerrors.BadRequest("PLAN_VALIDITY_UNIT_REQUIRED", "validity unit is required")
	}
	if req.OriginalPrice != nil && *req.OriginalPrice < 0 {
		return infraerrors.BadRequest("PLAN_ORIGINAL_PRICE_INVALID", "original price must be >= 0")
	}
	if req.Stock.Set {
		if err := validatePlanStock(req.Stock.Value); err != nil {
			return err
		}
	}
	if req.RenewalDiscountPercent.Set {
		if err := validatePlanRenewalDiscountPercentRange(req.RenewalDiscountPercent.Value); err != nil {
			return err
		}
	}
	if req.FiveHourLimitUSD.Set {
		if err := validatePlanRollingLimit("five_hour_limit_usd", req.FiveHourLimitUSD.Value); err != nil {
			return err
		}
	}
	if req.SevenDayLimitUSD.Set {
		if err := validatePlanRollingLimit("seven_day_limit_usd", req.SevenDayLimitUSD.Value); err != nil {
			return err
		}
	}
	if req.ThirtyDayLimitUSD.Set {
		if err := validatePlanRollingLimit("thirty_day_limit_usd", req.ThirtyDayLimitUSD.Value); err != nil {
			return err
		}
	}
	return nil
}

// --- Plan CRUD ---

// PlanGroupInfo holds the group details needed for subscription plan display.
type PlanGroupInfo struct {
	Platform           string   `json:"platform"`
	Name               string   `json:"name"`
	RateMultiplier     float64  `json:"rate_multiplier"`
	PeakRateEnabled    bool     `json:"peak_rate_enabled"`
	PeakStart          string   `json:"peak_start"`
	PeakEnd            string   `json:"peak_end"`
	PeakRateMultiplier float64  `json:"peak_rate_multiplier"`
	DailyLimitUSD      *float64 `json:"daily_limit_usd"`
	WeeklyLimitUSD     *float64 `json:"weekly_limit_usd"`
	MonthlyLimitUSD    *float64 `json:"monthly_limit_usd"`
	ModelScopes        []string `json:"supported_model_scopes"`
}

// GetGroupInfoMap returns a map of group_id → PlanGroupInfo for the given plans.
func (s *PaymentConfigService) GetGroupInfoMap(ctx context.Context, plans []*dbent.SubscriptionPlan) map[int64]PlanGroupInfo {
	ids := make([]int64, 0, len(plans))
	seen := make(map[int64]bool)
	for _, p := range plans {
		if !seen[p.GroupID] {
			seen[p.GroupID] = true
			ids = append(ids, p.GroupID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	groups, err := s.entClient.Group.Query().Where(group.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil
	}
	m := make(map[int64]PlanGroupInfo, len(groups))
	for _, g := range groups {
		m[int64(g.ID)] = PlanGroupInfo{
			Platform:           g.Platform,
			Name:               g.Name,
			RateMultiplier:     g.RateMultiplier,
			PeakRateEnabled:    g.PeakRateEnabled,
			PeakStart:          g.PeakStart,
			PeakEnd:            g.PeakEnd,
			PeakRateMultiplier: g.PeakRateMultiplier,
			DailyLimitUSD:      g.DailyLimitUsd,
			WeeklyLimitUSD:     g.WeeklyLimitUsd,
			MonthlyLimitUSD:    g.MonthlyLimitUsd,
			ModelScopes:        g.SupportedModelScopes,
		}
	}
	return m
}

func (s *PaymentConfigService) ListPlans(ctx context.Context) ([]*dbent.SubscriptionPlan, error) {
	return s.entClient.SubscriptionPlan.Query().Order(subscriptionplan.BySortOrder()).All(ctx)
}

func (s *PaymentConfigService) ListPlansForSale(ctx context.Context) ([]*dbent.SubscriptionPlan, error) {
	return s.entClient.SubscriptionPlan.Query().Where(subscriptionplan.ForSaleEQ(true)).Order(subscriptionplan.BySortOrder()).All(ctx)
}

func (s *PaymentConfigService) CreatePlan(ctx context.Context, req CreatePlanRequest) (*dbent.SubscriptionPlan, error) {
	if err := validatePlanRequired(req.Name, req.GroupID, req.Price, req.ValidityDays, req.ValidityUnit, req.OriginalPrice, req.FiveHourLimitUSD, req.SevenDayLimitUSD, req.ThirtyDayLimitUSD, req.RenewalDiscountPercent); err != nil {
		return nil, err
	}
	currency, err := normalizePlanCurrency(req.Currency)
	if err != nil {
		return nil, err
	}
	if err := validatePlanStock(req.Stock); err != nil {
		return nil, err
	}
	validityUnit := normalizePlanValidityUnit(req.ValidityUnit)
	b := s.entClient.SubscriptionPlan.Create().
		SetGroupID(req.GroupID).SetName(req.Name).SetDescription(req.Description).
		SetPrice(req.Price).SetCurrency(currency).SetValidityDays(req.ValidityDays).SetValidityUnit(validityUnit).
		SetFeatures(req.Features).SetProductName(req.ProductName).
		SetForSale(req.ForSale).SetSortOrder(req.SortOrder)
	if req.OriginalPrice != nil {
		b.SetOriginalPrice(*req.OriginalPrice)
	}
	if req.RenewalDiscountPercent != nil {
		b.SetRenewalDiscountPercent(*req.RenewalDiscountPercent)
	}
	b.SetNillableFiveHourLimitUsd(req.FiveHourLimitUSD)
	b.SetNillableSevenDayLimitUsd(req.SevenDayLimitUSD)
	b.SetNillableThirtyDayLimitUsd(req.ThirtyDayLimitUSD)
	b.SetNillableStock(req.Stock)
	return b.Save(ctx)
}

// UpdatePlan updates a subscription plan by ID (patch semantics).
// NOTE: This function exceeds 30 lines due to per-field nil-check patch update boilerplate
// plus a validation guard for non-nil fields.
func (s *PaymentConfigService) UpdatePlan(ctx context.Context, id int64, req UpdatePlanRequest) (*dbent.SubscriptionPlan, error) {
	if err := validatePlanPatch(req); err != nil {
		return nil, err
	}
	current, err := s.GetPlan(ctx, id)
	if err != nil {
		return nil, err
	}
	effectivePrice := current.Price
	if req.Price != nil {
		effectivePrice = *req.Price
	}
	effectiveRenewalDiscount := current.RenewalDiscountPercent
	if req.RenewalDiscountPercent.Set {
		effectiveRenewalDiscount = req.RenewalDiscountPercent.Value
	}
	if err := validatePlanRenewalDiscountPercent(effectiveRenewalDiscount, effectivePrice); err != nil {
		return nil, err
	}
	u := s.entClient.SubscriptionPlan.UpdateOneID(id)
	if req.GroupID != nil {
		u.SetGroupID(*req.GroupID)
	}
	if req.Name != nil {
		u.SetName(*req.Name)
	}
	if req.Description != nil {
		u.SetDescription(*req.Description)
	}
	if req.Price != nil {
		u.SetPrice(*req.Price)
	}
	if req.OriginalPrice != nil {
		u.SetOriginalPrice(*req.OriginalPrice)
	}
	if req.RenewalDiscountPercent.Set {
		if req.RenewalDiscountPercent.Value == nil {
			u.ClearRenewalDiscountPercent()
		} else {
			u.SetRenewalDiscountPercent(*req.RenewalDiscountPercent.Value)
		}
	}
	if req.FiveHourLimitUSD.Set {
		if req.FiveHourLimitUSD.Value == nil {
			u.ClearFiveHourLimitUsd()
		} else {
			u.SetFiveHourLimitUsd(*req.FiveHourLimitUSD.Value)
		}
	}
	if req.SevenDayLimitUSD.Set {
		if req.SevenDayLimitUSD.Value == nil {
			u.ClearSevenDayLimitUsd()
		} else {
			u.SetSevenDayLimitUsd(*req.SevenDayLimitUSD.Value)
		}
	}
	if req.ThirtyDayLimitUSD.Set {
		if req.ThirtyDayLimitUSD.Value == nil {
			u.ClearThirtyDayLimitUsd()
		} else {
			u.SetThirtyDayLimitUsd(*req.ThirtyDayLimitUSD.Value)
		}
	}
	if req.Stock.Set {
		if req.Stock.Value == nil {
			u.ClearStock()
		} else {
			u.SetStock(*req.Stock.Value)
		}
	}
	if req.Currency != nil {
		currency, err := normalizePlanCurrency(*req.Currency)
		if err != nil {
			return nil, err
		}
		u.SetCurrency(currency)
	}
	if req.ValidityDays != nil {
		u.SetValidityDays(*req.ValidityDays)
	}
	if req.ValidityUnit != nil {
		u.SetValidityUnit(normalizePlanValidityUnit(*req.ValidityUnit))
	}
	if req.Features != nil {
		u.SetFeatures(*req.Features)
	}
	if req.ProductName != nil {
		u.SetProductName(*req.ProductName)
	}
	if req.ForSale != nil {
		u.SetForSale(*req.ForSale)
	}
	if req.SortOrder != nil {
		u.SetSortOrder(*req.SortOrder)
	}
	return u.Save(ctx)
}

func (s *PaymentConfigService) DeletePlan(ctx context.Context, id int64) error {
	count, err := s.countPendingOrdersByPlan(ctx, id)
	if err != nil {
		return fmt.Errorf("check pending orders: %w", err)
	}
	if count > 0 {
		return infraerrors.Conflict("PENDING_ORDERS",
			fmt.Sprintf("this plan has %d in-progress orders and cannot be deleted — wait for orders to complete first", count))
	}
	return s.entClient.SubscriptionPlan.DeleteOneID(id).Exec(ctx)
}

// GetPlan returns a subscription plan by ID.
func (s *PaymentConfigService) GetPlan(ctx context.Context, id int64) (*dbent.SubscriptionPlan, error) {
	plan, err := s.entClient.SubscriptionPlan.Get(ctx, id)
	if err != nil {
		return nil, infraerrors.NotFound("PLAN_NOT_FOUND", "subscription plan not found")
	}
	return plan, nil
}
