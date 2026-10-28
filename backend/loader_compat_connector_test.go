package main

import (
	"testing"

	"shaurma-launcher-backend/internal/api"
	"shaurma-launcher-backend/internal/model"
)

// Fabric-мод у Forge-збірці з Sinytra Connector сумісний і позначений
// ViaConnector; без Connector — несумісний.
func TestAttachLoaderCompatConnector(t *testing.T) {
	mods := []model.ModEntry{
		{FileName: "connector.jar", ID: "connectormod", Enabled: true, Loader: "forge", Loaders: []string{"forge"}},
		{FileName: "axiom.jar", ID: "axiom", Enabled: true, Loader: "fabric", Loaders: []string{"fabric"}},
		{FileName: "create.jar", ID: "create", Enabled: true, Loader: "forge", Loaders: []string{"forge"}},
	}
	if !hasSinytraConnector(mods) {
		t.Fatal("connectormod not recognised as Sinytra Connector")
	}
	attachLoaderCompat(mods, "forge", true)
	if mods[1].Incompatible || !mods[1].ViaConnector {
		t.Fatalf("axiom: incompatible=%v via=%v", mods[1].Incompatible, mods[1].ViaConnector)
	}
	if mods[2].ViaConnector || mods[0].ViaConnector {
		t.Fatal("forge mods must not be marked via connector")
	}

	mods[1].Incompatible, mods[1].ViaConnector = false, false
	attachLoaderCompat(mods, "forge", false)
	if !mods[1].Incompatible || mods[1].ViaConnector {
		t.Fatal("without connector a fabric mod must be incompatible")
	}
}

// Автори проєкту організації: організація першою, власник організації —
// перший серед учасників, неприйняті учасники не показуються.
func TestModrinthCreatorsOrder(t *testing.T) {
	yes, no := true, false
	member := func(id, name, role string, owner bool, accepted *bool) api.ModrinthTeamMember {
		var m api.ModrinthTeamMember
		m.User.ID, m.User.Username, m.Role, m.IsOwner, m.Accepted = id, name, role, owner, accepted
		return m
	}
	org := &api.ModrinthOrganization{ID: "org1", Slug: "caffeinemc", Name: "CaffeineMC",
		Members: []api.ModrinthTeamMember{member("u2", "jelly", "Lead", true, &yes)}}
	got := modrinthCreators(org, []api.ModrinthTeamMember{
		member("u1", "ims", "Maintainer", false, &yes),
		member("u2", "jelly", "Project Lead", false, &yes),
		member("u3", "pending", "Dev", false, &no),
		member("u4", "douira", "Maintainer", false, nil),
	})
	names := []string{}
	for _, c := range got {
		names = append(names, c.Name)
	}
	want := []string{"CaffeineMC", "jelly", "douira", "ims"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("got %v, want %v", names, want)
		}
	}
	if !got[0].IsOrg || !got[1].IsOwner {
		t.Fatalf("flags wrong: %+v", got[:2])
	}
}
