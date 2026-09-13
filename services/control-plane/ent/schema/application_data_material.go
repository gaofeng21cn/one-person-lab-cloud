package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/index"
)

type ApplicationDataMaterial struct{ ent.Schema }

func (ApplicationDataMaterial) Fields() []ent.Field { return applicationDataMaterialFields() }

func (ApplicationDataMaterial) Indexes() []ent.Index {
	return []ent.Index{index.Fields("application_id", "version").Unique()}
}

func (ApplicationDataMaterial) Annotations() []schema.Annotation {
	return table("control_plane_application_data_materials")
}
