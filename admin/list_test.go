package admin

import "testing"

func TestPaginationLinksShowsAllPagesWhenFew(t *testing.T) {
	links := paginationLinks(2, 4)
	want := []int{1, 2, 3, 4}
	if len(links) != len(want) {
		t.Fatalf("got %d links, want %d", len(links), len(want))
	}
	for i, n := range want {
		if links[i].Ellipsis || links[i].Number != n {
			t.Fatalf("links[%d] = %+v, want Number=%d", i, links[i], n)
		}
	}
	if !links[1].Current {
		t.Fatalf("links[1] (page 2) should be Current")
	}
}

func TestPaginationLinksTruncatesWithEllipsisForManyPages(t *testing.T) {
	links := paginationLinks(10, 20)

	if links[0].Ellipsis || links[0].Number != 1 {
		t.Fatalf("first link = %+v, want page 1", links[0])
	}
	if !links[len(links)-1].Ellipsis && links[len(links)-1].Number != 20 {
		t.Fatalf("last link = %+v, want page 20", links[len(links)-1])
	}

	ellipsisCount := 0
	for _, l := range links {
		if l.Ellipsis {
			ellipsisCount++
		}
	}
	if ellipsisCount == 0 {
		t.Fatalf("expected at least one ellipsis marker for 20 pages, got links: %+v", links)
	}

	foundCurrent := false
	for _, l := range links {
		if l.Current {
			if l.Number != 10 {
				t.Fatalf("current link = %+v, want Number=10", l)
			}
			foundCurrent = true
		}
	}
	if !foundCurrent {
		t.Fatalf("no link marked Current among %+v", links)
	}
}

func TestPaginationLinksNoneForSinglePage(t *testing.T) {
	if links := paginationLinks(1, 1); links != nil {
		t.Fatalf("links = %+v, want nil for a single page", links)
	}
	if links := paginationLinks(1, 0); links != nil {
		t.Fatalf("links = %+v, want nil for zero pages", links)
	}
}
