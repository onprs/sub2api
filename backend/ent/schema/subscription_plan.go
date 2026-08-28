package schema

import (
	"fmt"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SubscriptionPlan holds the schema definition for the SubscriptionPlan entity.
//
// 删除策略：硬删除
// SubscriptionPlan 使用硬删除而非软删除，原因如下：
//   - 套餐为管理员维护的商品配置，删除即表示下架移除
//   - 通过 for_sale 字段控制是否在售，删除仅用于彻底移除
//   - 已购买的订阅记录保存在 UserSubscription 中，不受套餐删除影响
type SubscriptionPlan struct {
	ent.Schema
}

func validateNonNegativeFloat(name string) func(float64) error {
	return func(v float64) error {
		if v < 0 {
			return fmt.Errorf("%s must be greater than or equal to 0", name)
		}
		return nil
	}
}

func validatePositiveFloat(name string) func(float64) error {
	return func(v float64) error {
		if v <= 0 {
			return fmt.Errorf("%s must be greater than 0", name)
		}
		return nil
	}
}

func validateNonNegativeInt(name string) func(int) error {
	return func(v int) error {
		if v < 0 {
			return fmt.Errorf("%s must be greater than or equal to 0", name)
		}
		return nil
	}
}

func validateRenewalDiscountPercent(v float64) error {
	if v < 0 || v >= 100 {
		return fmt.Errorf("renewal_discount_percent must be greater than or equal to 0 and less than 100")
	}
	return nil
}

func (SubscriptionPlan) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "subscription_plans"},
	}
}

func (SubscriptionPlan) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("group_id"),
		field.String("name").
			MaxLen(100).
			NotEmpty(),
		field.String("description").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Default(""),
		field.Float("price").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,2)"}),
		field.Float("original_price").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,2)"}).
			Optional().
			Nillable(),
		field.Float("renewal_discount_percent").
			SchemaType(map[string]string{dialect.Postgres: "decimal(5,2)"}).
			Validate(validateRenewalDiscountPercent).
			Optional().
			Nillable(),
		field.Float("five_hour_limit_usd").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Validate(validateNonNegativeFloat("five_hour_limit_usd")).
			Optional().
			Nillable(),
		field.Float("seven_day_limit_usd").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Validate(validateNonNegativeFloat("seven_day_limit_usd")).
			Optional().
			Nillable(),
		field.Float("thirty_day_limit_usd").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Validate(validateNonNegativeFloat("thirty_day_limit_usd")).
			Optional().
			Nillable(),
		field.Int("stock").
			Validate(validateNonNegativeInt("stock")).
			Optional().
			Nillable(),
		field.String("currency").
			MaxLen(3).
			Default(""),
		field.Int("validity_days").
			Default(30),
		field.String("validity_unit").
			MaxLen(10).
			Default("day"),
		field.String("features").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Default(""),
		field.String("product_name").
			MaxLen(100).
			Default(""),
		field.Bool("for_sale").
			Default(true),
		field.Int("sort_order").
			Default(0),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (SubscriptionPlan) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("group_id"),
		index.Fields("for_sale"),
	}
}

func (SubscriptionPlan) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("redeem_codes", RedeemCode.Type),
		edge.To("user_subscriptions", UserSubscription.Type),
	}
}
