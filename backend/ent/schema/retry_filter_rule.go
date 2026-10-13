package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
)

type RetryFilterRule struct{ ent.Schema }

func (RetryFilterRule) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "retry_filter_rules"}}
}
func (RetryFilterRule) Mixin() []ent.Mixin { return []ent.Mixin{mixins.TimeMixin{}} }
func (RetryFilterRule) Fields() []ent.Field {
	jsonType := map[string]string{dialect.Postgres: "jsonb"}
	return []ent.Field{
		field.Int64("preset_id"),
		field.String("name").MaxLen(100).NotEmpty(),
		field.Bool("enabled").Default(true),
		field.Int("priority").Default(0),
		field.JSON("status_codes", []int{}).Default([]int{}).SchemaType(jsonType),
		field.JSON("keywords", []string{}).Default([]string{}).SchemaType(jsonType),
		field.JSON("upstream_codes", []string{}).Default([]string{}).SchemaType(jsonType),
		field.JSON("upstream_code_paths", []string{}).Default([]string{"error.type", "error.code", "code", "type"}).SchemaType(jsonType),
		field.JSON("models", []string{}).Default([]string{}).SchemaType(jsonType),
		field.String("match_mode").MaxLen(10).Default("any"),
		field.String("action").MaxLen(32).Default("same_account_retry"),
		field.Int("retry_count_override").Optional().Nillable(),
		field.Int("retry_delay_ms").Optional().Nillable(),
		field.String("empty_request_mode").MaxLen(16).Default("none"),
		field.String("empty_response_mode").MaxLen(16).Default("none"),
		field.Int("terminate_status_code").Optional().Nillable(),
		field.Text("terminate_message").Optional().Nillable(),
		field.Text("description").Optional().Nillable(),
	}
}
func (RetryFilterRule) Edges() []ent.Edge {
	return []ent.Edge{edge.From("preset", RetryFilterPreset.Type).Ref("rules").Field("preset_id").Unique().Required()}
}
