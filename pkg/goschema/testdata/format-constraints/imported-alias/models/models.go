package models

type Metadata struct{ Name *string }
type MetadataAlias = Metadata
type Object struct{ Metadata *MetadataAlias }
type ObjectAlias = Object
