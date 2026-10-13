package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
)

type RetryFilterPreset struct{ ent.Schema }

func (RetryFilterPreset) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "retry_filter_presets"}}
}
func (RetryFilterPreset) Mixin() []ent.Mixin { return []ent.Mixin{mixins.TimeMixin{}} }
func (RetryFilterPreset) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(100).NotEmpty(),
		field.Bool("enabled").Default(true),
		field.Text("description").Optional().Nillable(),
	}
}
func (RetryFilterPreset) Edges() []ent.Edge {
	return []ent.Edge{edge.To("rules", RetryFilterRule.Type).Annotations(entsql.OnDelete(entsql.Cascade))}
}
