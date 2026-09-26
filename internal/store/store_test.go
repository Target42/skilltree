package store

import (
	"database/sql"
	"testing"
)

func TestBuildTree(t *testing.T) {
	groups := []Group{
		{ID: 1, Name: "Root"},
		{ID: 2, Name: "Child", ParentID: sql.NullInt64{Int64: 1, Valid: true}},
		{ID: 3, Name: "Leaf", ParentID: sql.NullInt64{Int64: 2, Valid: true}},
		{ID: 4, Name: "Other"},
	}
	tree := BuildTree(groups, 3)
	if len(tree) != 2 {
		t.Fatalf("roots=%d", len(tree))
	}

	var root *TreeNode
	for i := range tree {
		if tree[i].Name == "Root" {
			root = &tree[i]
			break
		}
	}
	if root == nil {
		t.Fatal("root missing")
	}
	if !root.Open {
		t.Fatal("root should be open")
	}
	if len(root.Children) != 1 || root.Children[0].Name != "Child" {
		t.Fatalf("child=%v", root.Children)
	}
	if !root.Children[0].Open {
		t.Fatal("child should be open")
	}
	leaf := root.Children[0].Children[0]
	if leaf.Name != "Leaf" || !leaf.Active {
		t.Fatalf("leaf=%+v", leaf)
	}
}
