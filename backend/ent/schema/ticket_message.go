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

// TicketMessage stores an immutable public reply or administrator-only note.
type TicketMessage struct {
	ent.Schema
}

func (TicketMessage) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "ticket_messages"}}
}

func (TicketMessage) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("ticket_id"),
		field.Int64("author_id").Optional().Nillable(),
		field.String("author_role").MaxLen(16).Validate(validateTicketValue("ticket author role", allowedTicketValues("user", "admin", "system"))),
		field.String("visibility").MaxLen(16).Validate(validateTicketValue("ticket message visibility", allowedTicketValues("public", "internal"))),
		field.String("author_name").MaxLen(100).Default(""),
		field.String("body").SchemaType(map[string]string{dialect.Postgres: "text"}).NotEmpty().MaxLen(5000),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (TicketMessage) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("ticket", Ticket.Type).
			Ref("messages").
			Field("ticket_id").
			Unique().
			Required(),
		edge.From("author", User.Type).
			Ref("ticket_messages").
			Field("author_id").
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("attachments", TicketAttachment.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (TicketMessage) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("ticket_id"),
		index.Fields("ticket_id", "visibility"),
	}
}
