package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/index"
)

type ApplicationRevision struct{ ent.Schema }

func (ApplicationRevision) Fields() []ent.Field { return applicationRevisionFields() }

func (ApplicationRevision) Indexes() []ent.Index {
	return []ent.Index{index.Fields("application_id", "version").Unique()}
}

func (ApplicationRevision) Annotations() []schema.Annotation {
	return table("control_plane_application_revisions")
}
