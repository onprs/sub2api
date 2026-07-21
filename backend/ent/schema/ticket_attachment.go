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

// TicketAttachment stores private object metadata; object bytes live in a provider store.
type TicketAttachment struct {
	ent.Schema
}

func (TicketAttachment) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "ticket_attachments"}}
}

func (TicketAttachment) Fields() []ent.Field {
	return []ent.Field{
		field.String("upload_token").MaxLen(64).NotEmpty().Unique(),
		field.Int64("uploaded_by").Optional().Nillable(),
		field.String("uploader_role").MaxLen(16).Validate(validateTicketValue("ticket attachment uploader role", allowedTicketValues("user", "admin"))),
		field.Int64("ticket_id").Optional().Nillable(),
		field.Int64("message_id").Optional().Nillable(),
		field.String("state").MaxLen(16).Default("pending").Validate(validateTicketValue("ticket attachment state", allowedTicketValues("pending", "attached", "deleting"))),
		field.String("storage_provider").MaxLen(16).Validate(validateTicketValue("ticket attachment provider", allowedTicketValues("local", "s3"))),
		field.String("object_key").SchemaType(map[string]string{dialect.Postgres: "text"}).NotEmpty(),
		field.String("original_name").MaxLen(255).NotEmpty(),
		field.String("content_type").MaxLen(100).NotEmpty(),
		field.Int64("byte_size").Positive(),
		field.String("sha256").MaxLen(64).NotEmpty(),
		field.Time("expires_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (TicketAttachment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("uploader", User.Type).
			Ref("ticket_attachments").
			Field("uploaded_by").
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.From("ticket", Ticket.Type).
			Ref("attachments").
			Field("ticket_id").
			Unique(),
		edge.From("message", TicketMessage.Type).
			Ref("attachments").
			Field("message_id").
			Unique(),
	}
}

func (TicketAttachment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("storage_provider", "object_key").Unique(),
		index.Fields("state", "expires_at"),
		index.Fields("ticket_id", "message_id"),
		index.Fields("uploaded_by", "uploader_role", "created_at"),
	}
}
