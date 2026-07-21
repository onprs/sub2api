package schema

import (
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

var (
	ticketCategories = allowedTicketValues("api_issue", "subscription", "payment", "account", "feature_request", "other")
	ticketImpacts    = allowedTicketValues("blocked", "degraded", "general")
	ticketPriorities = allowedTicketValues("urgent", "high", "normal", "low")
	ticketStatuses   = allowedTicketValues("open", "in_progress", "waiting_user", "resolved", "closed")
)

func allowedTicketValues(values ...string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(values))
	for _, value := range values {
		allowed[value] = struct{}{}
	}
	return allowed
}

func validateTicketValue(fieldName string, allowed map[string]struct{}) func(string) error {
	return func(value string) error {
		if _, ok := allowed[value]; !ok {
			return fmt.Errorf("invalid %s %q", fieldName, value)
		}
		return nil
	}
}

// Ticket is the durable aggregate root for a support conversation.
type Ticket struct {
	ent.Schema
}

func (Ticket) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "tickets"}}
}

func (Ticket) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (Ticket) Fields() []ent.Field {
	return []ent.Field{
		field.String("ticket_no").MaxLen(32).NotEmpty().Unique(),
		field.Int64("user_id").Optional().Nillable(),
		field.String("requester_email").MaxLen(255).Default(""),
		field.String("requester_username").MaxLen(100).Default(""),
		field.String("subject").MaxLen(100).NotEmpty(),
		field.String("category").MaxLen(32).Validate(validateTicketValue("ticket category", ticketCategories)),
		field.String("impact").MaxLen(24).Validate(validateTicketValue("ticket impact", ticketImpacts)),
		field.String("priority").MaxLen(16).Default("normal").Validate(validateTicketValue("ticket priority", ticketPriorities)),
		field.String("status").MaxLen(24).Default("open").Validate(validateTicketValue("ticket status", ticketStatuses)),
		field.Int64("assignee_id").Optional().Nillable(),
		field.String("request_id").MaxLen(128).Default(""),
		field.Int64("usage_log_id").Optional().Nillable(),
		field.Int64("api_key_id").Optional().Nillable(),
		field.String("api_key_name").MaxLen(100).Default(""),
		field.Int64("payment_order_id").Optional().Nillable(),
		field.String("payment_order_no").MaxLen(100).Default(""),
		field.Int64("user_subscription_id").Optional().Nillable(),
		field.String("subscription_name").MaxLen(200).Default(""),
		field.Time("last_public_message_at").Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_activity_at").Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("action_required_since").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int64("user_notification_seq").Default(0).NonNegative(),
		field.Int64("user_last_read_seq").Default(0).NonNegative(),
		field.Time("resolved_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("reopen_deadline").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("closed_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int64("version").Default(1).Positive(),
	}
}

func (Ticket) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("requester", User.Type).
			Ref("requested_tickets").
			Field("user_id").
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.From("assignee", User.Type).
			Ref("assigned_tickets").
			Field("assignee_id").
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("messages", TicketMessage.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("events", TicketEvent.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("attachments", TicketAttachment.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Ticket) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "last_public_message_at"),
		index.Fields("user_id", "status", "last_public_message_at"),
		index.Fields("status", "priority", "action_required_since"),
		index.Fields("assignee_id", "status", "action_required_since"),
		index.Fields("category", "status", "last_activity_at"),
		index.Fields("request_id").Annotations(entsql.IndexWhere("request_id <> ''")),
		index.Fields("payment_order_id").Annotations(entsql.IndexWhere("payment_order_id IS NOT NULL")),
		index.Fields("user_subscription_id").Annotations(entsql.IndexWhere("user_subscription_id IS NOT NULL")),
		index.Fields("reopen_deadline").Annotations(entsql.IndexWhere("status = 'resolved'")),
	}
}
