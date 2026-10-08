// Package model reads tanGO models: plain Go structs whose fields, through
// their tango:"…" struct tags, describe a table's columns, primary key,
// unique and indexed columns, and foreign keys.
//
// Apps register their models with a [Registry], which validates each one
// and records its [ModelMeta] and [FieldMeta]. The rest of tanGO (the
// store, the migration generator and the admin) works from that metadata.
// See https://tangoframework.com/docs/guides/models-and-tags/.
package model
