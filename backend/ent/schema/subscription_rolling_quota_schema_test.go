package schema

import (
	"testing"

	"entgo.io/ent/entc/load"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionRollingQuotaSchemas(t *testing.T) {
	spec, err := (&load.Config{Path: "."}).Load()
	require.NoError(t, err)

	schemas := map[string]*load.Schema{}
	for _, schema := range spec.Schemas {
		schemas[schema.Name] = schema
	}

	plan := requireSchema(t, schemas, "SubscriptionPlan")
	requireSchemaFields(t, plan,
		"five_hour_limit_usd",
		"seven_day_limit_usd",
		"thirty_day_limit_usd",
	)

	subscription := requireSchema(t, schemas, "UserSubscription")
	requireSchemaFields(t, subscription,
		"five_hour_limit_usd",
		"seven_day_limit_usd",
		"thirty_day_limit_usd",
		"five_hour_usage_usd",
		"seven_day_usage_usd",
		"thirty_day_usage_usd",
		"five_hour_window_start",
		"seven_day_window_start",
		"thirty_day_window_start",
	)
}
