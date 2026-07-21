package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TicketEvent stores immutable structured audit events for a ticket.
type TicketEvent struct {
	ent.Schema
}

func (TicketEvent) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "ticket_events"}}
}

func (TicketEvent) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("ticket_id"),
		field.Int64("actor_id").Optional().Nillable(),
		field.String("actor_role").MaxLen(16).Validate(validateTicketValue("ticket actor role", allowedTicketValues("user", "admin", "system"))),
		field.String("event_type").MaxLen(40).NotEmpty(),
		field.String("from_status").MaxLen(24).Optional().Nillable().Validate(validateTicketValue("ticket event source status", ticketStatuses)),
		field.String("to_status").MaxLen(24).Optional().Nillable().Validate(validateTicketValue("ticket event target status", ticketStatuses)),
		field.JSON("payload", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.String("visibility").MaxLen(16).Validate(validateTicketValue("ticket event visibility", allowedTicketValues("public", "internal"))),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (TicketEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("ticket", Ticket.Type).
			Ref("events").
			Field("ticket_id").
			Unique().
			Required(),
		edge.From("actor", User.Type).
			Ref("ticket_events").
			Field("actor_id").
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

func (TicketEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("ticket_id"),
		index.Fields("ticket_id", "visibility"),
	}
}
