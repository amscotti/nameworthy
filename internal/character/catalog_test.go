package character

import (
	"slices"
	"strings"
	"testing"
)

// Independent transcription of Blizzard's Forever matrix. Test every cell,
// including the six additions and Skyborne's faction-specific restrictions.
func TestForeverMatrix(t *testing.T) {
	want := map[string]string{
		"Human":                 "Warrior,Paladin,Hunter,Rogue,Priest,Mage,Warlock",
		"Dwarf":                 "Warrior,Paladin,Hunter,Rogue,Priest,Shaman",
		"Night Elf":             "Warrior,Hunter,Rogue,Priest,Druid",
		"Gnome":                 "Warrior,Rogue,Priest,Mage,Warlock",
		"Skyborne (High Order)": "Warrior,Hunter,Rogue,Mage,Druid",
		"Orc":                   "Warrior,Hunter,Rogue,Shaman,Mage,Warlock",
		"Undead":                "Warrior,Paladin,Rogue,Priest,Mage,Warlock",
		"Tauren":                "Warrior,Hunter,Shaman,Druid",
		"Troll":                 "Warrior,Hunter,Rogue,Priest,Shaman,Mage,Warlock",
		"Skyborne (Windshaper)": "Warrior,Hunter,Rogue,Shaman,Druid",
	}
	if got := All(); len(got.Races) != 10 || len(got.Classes) != 9 {
		t.Fatalf("roster = %d race options / %d classes", len(got.Races), len(got.Classes))
	}
	for race, allowed := range want {
		for _, class := range All().Classes {
			_, _, err := Resolve(race, class.Name)
			if valid := slices.Contains(strings.Split(allowed, ","), class.Name); (err == nil) != valid {
				t.Errorf("%s %s: err = %v, playable = %v", race, class.Name, err, valid)
			}
		}
	}
}

func TestOptionalAndCanonicalContext(t *testing.T) {
	for _, pair := range [][2]string{{"", ""}, {"Night Elf", ""}, {"", "Druid"}} {
		if _, _, err := Resolve(pair[0], pair[1]); err != nil {
			t.Errorf("optional context %v: %v", pair, err)
		}
	}
	race, class, err := Resolve("undead", "paladin")
	if err != nil || race != "Undead" || class != "Paladin" {
		t.Fatalf("canonical context = %q, %q, %v", race, class, err)
	}
	for _, pair := range [][2]string{{"Blood Elf", "Paladin"}, {"", "Evoker"}, {"Skyborne", "Mage"}} {
		if _, _, err := Resolve(pair[0], pair[1]); err == nil {
			t.Errorf("accepted non-Forever or ambiguous context %v", pair)
		}
	}
}

func TestCatalogReturnsDefensiveCopies(t *testing.T) {
	cat := All()
	cat.Races[0].Classes[0] = "Mutated"
	cat.Races[0].NewClasses[0] = "Mutated"

	r, ok := ByRace("Human")
	if !ok {
		t.Fatal("missing Human")
	}
	r.Classes[0] = "Mutated"

	if _, _, err := Resolve("Human", "Warrior"); err != nil {
		t.Fatalf("catalog slice mutation leaked into canonical roster: %v", err)
	}
	if got := All().Races[0].NewClasses[0]; got != "Hunter" {
		t.Fatalf("NewClasses mutated to %q, want Hunter", got)
	}
}
