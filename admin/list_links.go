package admin

import (
	"maps"
	"net/url"
	"strconv"
)

// listParams is the page state every changelist link carries: the search
// query, when set, and the applied filters.
func listParams(search string, applied url.Values) url.Values {
	params := make(url.Values, len(applied)+1)
	maps.Copy(params, applied)
	if search != "" {
		params.Set("q", search)
	}
	return params
}

// pageHref is the changelist link to page, keeping params. url.Values
// encodes every value, so a search query with &, # or + survives the round
// trip.
func pageHref(params url.Values, page int) string {
	next := make(url.Values, len(params)+1)
	maps.Copy(next, params)
	next.Set("page", strconv.Itoa(page))
	return "?" + next.Encode()
}

// withPageHrefs returns a copy of links where every numbered page carries its
// changelist href. Ellipsis markers keep an empty Href.
func withPageHrefs(links []pageLink, params url.Values) []pageLink {
	out := make([]pageLink, len(links))
	for i, link := range links {
		if !link.Ellipsis {
			link.Href = pageHref(params, link.Number)
		}
		out[i] = link
	}
	return out
}
