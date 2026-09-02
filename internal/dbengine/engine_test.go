package dbengine_test

import (
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/dbengine"
)

func TestInstanceEnginesAreASubsetOfCatalogEngines(t *testing.T) {
	for _, engine := range dbengine.InstanceEngines() {
		if !engine.ValidForCatalog() {
			t.Errorf("instance engine %q is not a catalogue engine; the two value sets have drifted apart", engine)
		}
	}
}

func TestAgnosticIsCatalogueOnly(t *testing.T) {
	if dbengine.Agnostic.ValidForInstance() {
		t.Fatal("AGNOSTIC must never be a valid instance engine: an instance always connects to one product")
	}
	if !dbengine.Agnostic.ValidForCatalog() {
		t.Fatal("AGNOSTIC must be a valid catalogue engine: host/agent/collector metrics belong to no product")
	}
}

func TestUnknownAndEmptyEnginesAreInvalid(t *testing.T) {
	for _, engine := range []dbengine.Engine{"", "MYSQL", "postgresql"} {
		if engine.ValidForInstance() || engine.ValidForCatalog() {
			t.Errorf("engine %q must not be valid anywhere", engine)
		}
	}
}

func TestCatalogEnginesIsNotAliasedOntoInstanceEngines(t *testing.T) {
	// append 复用底层数组会让 CatalogEngines 悄悄改写 InstanceEngines 的返回值。
	catalog := dbengine.CatalogEngines()
	catalog[0] = "MYSQL"
	if got := dbengine.InstanceEngines()[0]; got != dbengine.PostgreSQL {
		t.Fatalf("InstanceEngines() was mutated through CatalogEngines(): got %q", got)
	}
}
