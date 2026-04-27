package schema

import "testing"

func TestResolverRoundtrip(t *testing.T) {
	r := NewResolver()
	for _, def := range SeedEntityTypes("did:key:peer1") {
		r.RegisterEntityType("did:key:peer1", def)
	}
	got, err := r.EntityType("did:key:peer1", "Project")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Project" {
		t.Fatal("wrong def returned")
	}
	if _, err := r.EntityType("did:key:peer1", "Nope"); err == nil {
		t.Fatal("expected miss")
	}
	if _, err := r.EntityType("did:key:other", "Project"); err == nil {
		t.Fatal("expected peer-isolated miss")
	}
	if len(r.EntityTypes("did:key:peer1")) == 0 {
		t.Fatal("expected types listed")
	}
}

func TestResolverRelations(t *testing.T) {
	r := NewResolver()
	for _, def := range SeedRelationshipTypes("did:key:p") {
		r.RegisterRelationType("did:key:p", def)
	}
	if _, err := r.RelationType("did:key:p", "depends_on"); err != nil {
		t.Fatal(err)
	}
	if len(r.RelationTypes("did:key:p")) != 3 {
		t.Fatal("expected 3 seed relationship types")
	}
}
