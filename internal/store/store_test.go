package store

import (
	"errors"
	"testing"

	"github.com/pblumer/clio-workbench/internal/model"
)

func newDraft(id string) *model.Draft {
	return &model.Draft{
		ID:        id,
		Name:      "Order",
		Kind:      model.KindEntity,
		Namespace: "order",
		Nodes:     []model.Node{{ID: "created", Label: "created", Start: true}},
		Edges:     []model.Edge{},
	}
}

func TestCreateGetAndList(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	d := newDraft("order")
	if err := s.Create(d); err != nil {
		t.Fatalf("create: %v", err)
	}
	if d.CreatedAt.IsZero() || d.UpdatedAt.IsZero() {
		t.Fatal("create should stamp timestamps")
	}

	got, err := s.Get("order")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Order" || len(got.Nodes) != 1 {
		t.Fatalf("unexpected draft: %+v", got)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 draft, got %d", len(list))
	}
}

func TestCreateRejectsDuplicate(t *testing.T) {
	s, _ := Open(t.TempDir())
	if err := s.Create(newDraft("order")); err != nil {
		t.Fatalf("create: %v", err)
	}
	err := s.Create(newDraft("order"))
	if !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
}

func TestSavePreservesCreatedAt(t *testing.T) {
	s, _ := Open(t.TempDir())
	d := newDraft("order")
	if err := s.Create(d); err != nil {
		t.Fatalf("create: %v", err)
	}
	created := d.CreatedAt

	d.Name = "Order v2"
	if err := s.Save(d); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, _ := s.Get("order")
	if !got.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt changed: %v != %v", got.CreatedAt, created)
	}
	if !got.UpdatedAt.After(created) && !got.UpdatedAt.Equal(created) {
		t.Fatal("UpdatedAt should advance")
	}
	if got.Name != "Order v2" {
		t.Fatalf("save did not persist change: %q", got.Name)
	}
}

func TestGetAndDeleteNotFound(t *testing.T) {
	s, _ := Open(t.TempDir())
	if _, err := s.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get: want ErrNotFound, got %v", err)
	}
	if err := s.Delete("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete: want ErrNotFound, got %v", err)
	}
}

func TestCreateRejectsInvalid(t *testing.T) {
	s, _ := Open(t.TempDir())
	bad := newDraft("Order Caps") // invalid slug
	if err := s.Create(bad); err == nil {
		t.Fatal("expected validation error for invalid id")
	}
}
