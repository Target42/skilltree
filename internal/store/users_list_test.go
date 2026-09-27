package store

import "testing"

func TestListUsersFilterNormalized(t *testing.T) {
	f := ListUsersFilter{Query: "  alice  ", Letter: "B", Page: 0, PageSize: 0}.Normalized()
	if f.Query != "alice" || f.Letter != "b" || f.Page != 1 || f.PageSize != DefaultUsersPageSize {
		t.Fatalf("got %+v", f)
	}
	f = ListUsersFilter{Letter: "1", PageSize: 500}.Normalized()
	if f.Letter != "" || f.PageSize != 100 {
		t.Fatalf("got %+v", f)
	}
}

func TestLikeContains(t *testing.T) {
	if got := likeContains(`a%b_c\d`); got != `%a\%b\_c\\d%` {
		t.Fatalf("got %q", got)
	}
}
