// Package openapi describes an app's HTTP routes as an OpenAPI 3.1 document.
//
// Install New in InstalledApps to serve the document; nothing is served unless
// you do. Every route included in the registry is listed with its method,
// path, path parameters and name. A route can add detail with tango.Doc.
package openapi
