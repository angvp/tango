package admin

import "github.com/angvp/tango"

// hiddenPage keeps the admin's HTML pages and assets out of generated API
// documentation (the openapi package): they are for browsers, not part of an
// app's API.
var hiddenPage = tango.Doc(tango.RouteDoc{Hidden: true})
